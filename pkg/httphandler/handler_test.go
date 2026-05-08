package httphandler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/http-pg/http-pg/pkg/crypto"
	"github.com/http-pg/http-pg/pkg/pgpool"
)

type mockPoolManager struct{}

func (m *mockPoolManager) AcquireSession(ctx context.Context, sessionID string) (*pgxpool.Conn, error) {
	return nil, nil
}

func (m *mockPoolManager) ReleaseSession(sessionID string) {}

func (m *mockPoolManager) GetSessionConn(sessionID string) (*pgxpool.Conn, bool) {
	return nil, false
}

func (m *mockPoolManager) GetStats() pgpool.PoolStats {
	return pgpool.PoolStats{
		TotalConns:    5,
		AcquiredConns: 2,
		IdleConns:     3,
		MaxConns:      20,
	}
}

func (m *mockPoolManager) Close() {}

func (m *mockPoolManager) ExecSQL(ctx context.Context, sessionID, sql string) (pgpool.QueryResult, error) {
	return pgpool.QueryResult{
		Columns:      []string{"id", "name"},
		Rows:         [][]string{{"1", "Alice"}},
		RowsAffected: 1,
	}, nil
}

func (m *mockPoolManager) ExecCommand(ctx context.Context, sessionID, sql string) (int64, error) {
	return 1, nil
}

func setupTestRouter(t *testing.T) (*gin.Engine, string) {
	gin.SetMode(gin.TestMode)

	key := crypto.GenerateKey()

	r := gin.New()
	api := r.Group("/api/v1")

	handler, _ := NewHandler(&mockPoolManager{}, key)

	api.GET("/health", handler.HealthCheck)
	api.POST("/session", handler.CreateSession)
	api.POST("/query", handler.HandleQuery)
	api.DELETE("/session/:session_id", handler.CloseSession)

	return r, key
}

func TestHealthCheck(t *testing.T) {
	r, _ := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "healthy" {
		t.Errorf("expected status 'healthy', got %v", resp["status"])
	}
}

func TestCreateSession(t *testing.T) {
	r, key := setupTestRouter(t)
	encKey, _ := crypto.DeriveKey(key)

	encrypted, _ := crypto.Encrypt([]byte("startup params"), encKey)

	req := httptest.NewRequest("POST", "/api/v1/session", strings.NewReader(string(encrypted)))
	req.Header.Set("Content-Type", "application/octet-stream")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	sessionID := w.Body.String()
	if len(sessionID) == 0 {
		t.Error("expected non-empty session ID")
	}
}

func TestHandleQuery(t *testing.T) {
	r, key := setupTestRouter(t)
	encKey, _ := crypto.DeriveKey(key)

	reqBody, _ := json.Marshal(map[string]interface{}{
		"type": byte('Q'),
		"sql":  "SELECT 1;",
	})
	encrypted, _ := crypto.Encrypt(reqBody, encKey)

	req := httptest.NewRequest("POST", "/api/v1/query", strings.NewReader(string(encrypted)))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Session-ID", "test-session-123")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
		return
	}

	// Decrypt the response
	decrypted, err := crypto.Decrypt(w.Body.Bytes(), encKey)
	if err != nil {
		t.Fatalf("decrypt response failed: %v", err)
	}

	var result pgpool.QueryResult
	if err := json.Unmarshal(decrypted, &result); err != nil {
		t.Fatalf("unmarshal result failed: %v", err)
	}

	if len(result.Columns) != 2 || result.Columns[0] != "id" {
		t.Errorf("unexpected columns: %v", result.Columns)
	}
	if len(result.Rows) != 1 || result.Rows[0][1] != "Alice" {
		t.Errorf("unexpected rows: %v", result.Rows)
	}
}

func TestHandleQueryMissingSession(t *testing.T) {
	r, key := setupTestRouter(t)
	encKey, _ := crypto.DeriveKey(key)

	reqBody, _ := json.Marshal(map[string]interface{}{
		"type": byte('Q'),
		"sql":  "SELECT 1;",
	})
	encrypted, _ := crypto.Encrypt(reqBody, encKey)

	req := httptest.NewRequest("POST", "/api/v1/query", strings.NewReader(string(encrypted)))
	req.Header.Set("Content-Type", "application/octet-stream")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleQueryNoEncryption(t *testing.T) {
	r, _ := setupTestRouter(t)

	req := httptest.NewRequest("POST", "/api/v1/query", strings.NewReader("raw unencrypted data"))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Session-ID", "test-session")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for unencrypted data, got %d", w.Code)
	}
}

func TestCloseSession(t *testing.T) {
	r, _ := setupTestRouter(t)

	req := httptest.NewRequest("DELETE", "/api/v1/session/test-session", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHealthCheck_VerifyResponse(t *testing.T) {
	r, _ := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	fields := []string{"status", "total_conns", "acquired_conns", "idle_conns", "max_conns"}
	for _, field := range fields {
		if _, ok := resp[field]; !ok {
			t.Errorf("missing field %q in health response", field)
		}
	}
}
