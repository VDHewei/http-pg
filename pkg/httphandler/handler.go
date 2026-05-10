package httphandler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VDHewei/http-pg/pkg/crypto"
	"github.com/VDHewei/http-pg/pkg/pgpool"
)

// Pool is the interface for database pool operations.
type Pool interface {
	AcquireSession(ctx context.Context, sessionID string) (*pgxpool.Conn, error)
	ReleaseSession(sessionID string)
	GetSessionConn(sessionID string) (*pgxpool.Conn, bool)
	GetStats() pgpool.PoolStats
	Close()
	ExecSQL(ctx context.Context, sessionID, sql string) (pgpool.QueryResult, error)
	ExecCommand(ctx context.Context, sessionID, sql string) (int64, error)
}

// Handler handles HTTP API requests for PgSQL proxying.
type Handler struct {
	pool   Pool
	encKey []byte
	stmts  map[string]string // sessionID -> cached statement SQL (unnamed)
}

// NewHandler creates a new Handler.
func NewHandler(pool Pool, encKey string) (*Handler, error) {
	key, err := crypto.DeriveKey(encKey)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}

	return &Handler{
		pool:   pool,
		encKey: key,
		stmts:  make(map[string]string),
	}, nil
}

// CreateSession creates a new PgSQL session.
func (h *Handler) CreateSession(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body: " + err.Error()})
		return
	}

	// Decrypt startup parameters
	decrypted, err := crypto.Decrypt(body, h.encKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "decrypt: " + err.Error()})
		return
	}
	_ = decrypted

	sessionID := uuid.New().String()

	if _, err := h.pool.AcquireSession(c.Request.Context(), sessionID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "acquire connection: " + err.Error()})
		return
	}

	c.String(http.StatusOK, sessionID)
}

// HandleQuery processes a proxied PgSQL message.
func (h *Handler) HandleQuery(c *gin.Context) {
	sessionID := c.GetHeader("X-Session-ID")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing X-Session-ID header"})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body: " + err.Error()})
		return
	}

	decrypted, err := crypto.Decrypt(body, h.encKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "decrypt: " + err.Error()})
		return
	}

	var req pgMessageRequest
	if err := json.Unmarshal(decrypted, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unmarshal request: " + err.Error()})
		return
	}

	log.Printf("[Session %s] SQL: %s", sessionID[:8], req.SQL)

	// Handle extended query protocol: Parse and Execute
	if req.Type == 'P' {
		// Parse: cache the SQL for unnamed statement
		h.stmts[sessionID] = req.SQL
		// Return empty response (just acknowledge parse was successful)
		result := pgpool.QueryResult{}
		resultJSON, _ := json.Marshal(result)
		encrypted, err := crypto.Encrypt(resultJSON, h.encKey)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "encrypt: " + err.Error()})
			return
		}
		c.Data(http.StatusOK, "application/octet-stream", encrypted)
		return
	}

	if req.Type == 'E' || req.Type == 'B' {
		// Bind/Execute: use cached SQL from previous Parse
		cachedSQL, ok := h.stmts[sessionID]
		if !ok || cachedSQL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no parsed statement found"})
			return
		}
		req.SQL = cachedSQL
	}

	cmdType := sqlCommandType(req.SQL)
	responseJSON, err := h.executeSQL(c.Request.Context(), sessionID, req.SQL, cmdType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "exec sql: " + err.Error()})
		return
	}

	encrypted, err := crypto.Encrypt(responseJSON, h.encKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "encrypt response: " + err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/octet-stream", encrypted)
}

// executeSQL routes SQL execution to the appropriate method based on command type.
func (h *Handler) executeSQL(ctx context.Context, sessionID, sql, cmdType string) ([]byte, error) {
	switch cmdType {
	case "SELECT", "SHOW", "EXPLAIN", "DESCRIBE", "WITH":
		return h.executeQuery(ctx, sessionID, sql)
	case "INSERT", "UPDATE", "DELETE", "CREATE", "ALTER", "DROP", "TRUNCATE",
		"SET", "BEGIN", "COMMIT", "ROLLBACK", "GRANT", "REVOKE", "COPY", "VACUUM":
		return h.executeCommand(ctx, sessionID, sql)
	default:
		return h.executeQuery(ctx, sessionID, sql)
	}
}

func (h *Handler) executeQuery(ctx context.Context, sessionID, sql string) ([]byte, error) {
	result, err := h.pool.ExecSQL(ctx, sessionID, sql)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func (h *Handler) executeCommand(ctx context.Context, sessionID, sql string) ([]byte, error) {
	rowsAffected, err := h.pool.ExecCommand(ctx, sessionID, sql)
	if err != nil {
		return nil, err
	}
	result := pgpool.QueryResult{
		RowsAffected: rowsAffected,
	}
	return json.Marshal(result)
}

// sqlCommandType extracts the first word (command type) from a SQL statement.
func sqlCommandType(sql string) string {
	i := 0
	for i < len(sql) && (sql[i] == ' ' || sql[i] == '\t' || sql[i] == '\n' || sql[i] == '\r') {
		i++
	}
	j := i
	for j < len(sql) && sql[j] != ' ' && sql[j] != '\t' && sql[j] != '\n' &&
		sql[j] != '\r' && sql[j] != '(' && sql[j] != ';' {
		j++
	}
	if j <= i {
		return ""
	}
	return sql[i:j]
}

// CloseSession closes a PgSQL session.
func (h *Handler) CloseSession(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing session_id"})
		return
	}

	h.pool.ReleaseSession(sessionID)
	delete(h.stmts, sessionID)
	c.JSON(http.StatusOK, gin.H{"status": "closed"})
}

// HealthCheck returns server health status.
func (h *Handler) HealthCheck(c *gin.Context) {
	stats := h.pool.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"status":         "healthy",
		"total_conns":    stats.TotalConns,
		"acquired_conns": stats.AcquiredConns,
		"idle_conns":     stats.IdleConns,
		"max_conns":      stats.MaxConns,
	})
}

type pgMessageRequest struct {
	Type byte   `json:"type"`
	SQL  string `json:"sql"`
	Raw  []byte `json:"raw,omitempty"`
}
