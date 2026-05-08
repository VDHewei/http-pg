package httpclient

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/http-pg/http-pg/pkg/crypto"
)

func TestNewClient(t *testing.T) {
	key := crypto.GenerateKey()
	client, err := NewClient("http://localhost:8080", key)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestNewClientEmptyKey(t *testing.T) {
	_, err := NewClient("http://localhost:8080", "")
	if err == nil {
		t.Fatal("expected error for empty key, got nil")
	}
}

func TestSessionRequest(t *testing.T) {
	key := crypto.GenerateKey()

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		body, _ := io.ReadAll(r.Body)
		decKey, _ := crypto.DeriveKey(key)
		decrypted, err := crypto.Decrypt(body, decKey)
		if err != nil {
			http.Error(w, "decrypt error", http.StatusBadRequest)
			return
		}
		_ = decrypted

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test-session-id-001"))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, key)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	sessionID, err := client.SessionRequest([]byte("startup params"))
	if err != nil {
		t.Fatalf("SessionRequest failed: %v", err)
	}

	if sessionID != "test-session-id-001" {
		t.Errorf("expected 'test-session-id-001', got %q", sessionID)
	}
}

func TestQueryRequest(t *testing.T) {
	key := crypto.GenerateKey()
	decKey, _ := crypto.DeriveKey(key)

	expectedSQL := "SELECT * FROM users"

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		sessionID := r.Header.Get("X-Session-ID")
		if sessionID == "" {
			http.Error(w, "missing session id", http.StatusBadRequest)
			return
		}

		body, _ := io.ReadAll(r.Body)
		decrypted, err := crypto.Decrypt(body, decKey)
		if err != nil {
			http.Error(w, "decrypt error: "+err.Error(), http.StatusBadRequest)
			return
		}

		var req pgMessageRequest
		if err := json.Unmarshal(decrypted, &req); err != nil {
			http.Error(w, "unmarshal error", http.StatusBadRequest)
			return
		}

		// Return a mock result
		result := map[string]interface{}{
			"columns":       []string{"id", "name"},
			"rows":          [][]string{{"1", "Alice"}},
			"rows_affected": int64(1),
		}
		resultJSON, _ := json.Marshal(result)

		encrypted, _ := crypto.Encrypt(resultJSON, decKey)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(encrypted)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, key)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	reqJSON, _ := json.Marshal(map[string]interface{}{
		"type": byte('Q'),
		"sql":  expectedSQL,
	})
	respBytes, err := client.QueryRequest("session-001", reqJSON)
	if err != nil {
		t.Fatalf("QueryRequest failed: %v", err)
	}

	// QueryRequest already decrypts internally, so respBytes is the plain JSON
	var result map[string]interface{}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		t.Fatalf("Unmarshal result failed: %v", err)
	}

	cols := result["columns"].([]interface{})
	if len(cols) != 2 || cols[0].(string) != "id" {
		t.Errorf("unexpected columns: %v", cols)
	}
}

func TestCloseSession(t *testing.T) {
	key := crypto.GenerateKey()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/session/test-001" && r.Method == "DELETE" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, key)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	err = client.CloseSession("test-001")
	if err != nil {
		t.Fatalf("CloseSession failed: %v", err)
	}
}

func TestQueryRequest_ServerError(t *testing.T) {
	key := crypto.GenerateKey()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, key)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	_, err = client.QueryRequest("session-001", []byte("test"))
	if err == nil {
		t.Fatal("expected error for server error, got nil")
	}
}

// pgMessageRequest is a local copy for testing
type pgMessageRequest struct {
	Type byte   `json:"type"`
	SQL  string `json:"sql"`
}
