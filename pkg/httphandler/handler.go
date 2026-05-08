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

	"github.com/http-pg/http-pg/pkg/crypto"
	"github.com/http-pg/http-pg/pkg/pgpool"
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

	result, err := h.pool.ExecSQL(c.Request.Context(), sessionID, req.SQL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "exec sql: " + err.Error()})
		return
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "marshal result: " + err.Error()})
		return
	}

	encrypted, err := crypto.Encrypt(resultJSON, h.encKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "encrypt response: " + err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/octet-stream", encrypted)
}

// CloseSession closes a PgSQL session.
func (h *Handler) CloseSession(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing session_id"})
		return
	}

	h.pool.ReleaseSession(sessionID)
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
