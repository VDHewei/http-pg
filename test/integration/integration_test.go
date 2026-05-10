//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"

	"github.com/VDHewei/http-pg/internal/config"
	"github.com/VDHewei/http-pg/pkg/crypto"
	"github.com/VDHewei/http-pg/pkg/httphandler"
	"github.com/VDHewei/http-pg/pkg/httpclient"
	"github.com/VDHewei/http-pg/pkg/pgparser"
	"github.com/VDHewei/http-pg/pkg/pgpool"
)

// TestEndToEnd tests the full flow: PgSQL Client -> Proxy -> HTTP Server -> PostgreSQL -> back
func TestEndToEnd(t *testing.T) {
	// Check for INTEGRATION_TEST env var
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("Skipping integration test. Set INTEGRATION_TEST=1 to run.")
	}

	postgresDSN := os.Getenv("POSTGRES_DSN")
	if postgresDSN == "" {
		postgresDSN = "postgres://postgres:postgres@localhost:5432/testdb?sslmode=disable"
	}

	encryptionKey := os.Getenv("ENCRYPTION_KEY")
	if encryptionKey == "" {
		encryptionKey = crypto.GenerateKey()
	}

	// Setup: start HTTP server
	ctx := context.Background()
	pool, err := pgpool.New(ctx, postgresDSN, 10, 2)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	// Create test table
	conn, err := pgx.Connect(ctx, postgresDSN)
	if err != nil {
		t.Fatalf("Failed to connect directly: %v", err)
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, `
		DROP TABLE IF EXISTS http_pg_test;
		CREATE TABLE http_pg_test (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			value INT NOT NULL
		);
		INSERT INTO http_pg_test (name, value) VALUES ('alpha', 100), ('beta', 200);
	`)
	if err != nil {
		t.Fatalf("Failed to create test data: %v", err)
	}

	// Start Gin server on a random port
	handler, err := httphandler.NewHandler(pool, encryptionKey)
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api/v1")
	api.GET("/health", handler.HealthCheck)
	api.POST("/session", handler.CreateSession)
	api.POST("/query", handler.HandleQuery)
	api.DELETE("/session/:session_id", handler.CloseSession)

	serverPort := findFreePort()
	serverAddr := "localhost:" + serverPort

	go r.Run(":" + serverPort)
	time.Sleep(500 * time.Millisecond)

	// Setup: create HTTP client
	httpClient, err := httpclient.NewClient("http://"+serverAddr, encryptionKey)
	if err != nil {
		t.Fatalf("Failed to create HTTP client: %v", err)
	}

	// Test 1: Health check
	resp, err := doRequest("GET", "http://"+serverAddr+"/api/v1/health", nil, "")
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("Health check returned %d", resp.StatusCode)
	}
	t.Log("Health check passed")

	// Test 2: Create session
	sessionID, err := httpClient.SessionRequest([]byte("startup"), "pg")
	if err != nil {
		t.Fatalf("Create session failed: %v", err)
	}
	t.Logf("Session created: %s", sessionID[:8])

	// Test 3: Query via HTTP
	reqJSON, _ := json.Marshal(map[string]interface{}{
		"type": byte('Q'),
		"sql":  "SELECT * FROM http_pg_test ORDER BY id;",
	})
	respBytes, err := httpClient.QueryRequest(sessionID, reqJSON)
	if err != nil {
		t.Fatalf("Query request failed: %v", err)
	}

	var result pgParserResult
	if err := json.Unmarshal(respBytes, &result); err != nil {
		t.Fatalf("Unmarshal result failed: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Fatalf("Expected 2 rows, got %d", len(result.Rows))
	}
	if result.Rows[0][1] != "alpha" {
		t.Errorf("Expected 'alpha', got %q", result.Rows[0][1])
	}
	if result.Rows[1][1] != "beta" {
		t.Errorf("Expected 'beta', got %q", result.Rows[1][1])
	}
	t.Logf("Query result: %d rows returned", len(result.Rows))

	// Test 4: Terminate session
	err = httpClient.CloseSession(sessionID)
	if err != nil {
		t.Fatalf("Close session failed: %v", err)
	}
	t.Log("Session closed successfully")
}

// TestProxyStartup simulates a PgSQL client connecting and sending queries
func TestProxyStartup(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("Skipping integration test. Set INTEGRATION_TEST=1 to run.")
	}

	postgresDSN := os.Getenv("POSTGRES_DSN")
	if postgresDSN == "" {
		postgresDSN = "postgres://postgres:postgres@localhost:5432/testdb?sslmode=disable"
	}

	encryptionKey := os.Getenv("ENCRYPTION_KEY")
	if encryptionKey == "" {
		encryptionKey = crypto.GenerateKey()
	}

	ctx := context.Background()
	pool, err := pgpool.New(ctx, postgresDSN, 10, 2)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	// Setup test data
	conn, err := pgx.Connect(ctx, postgresDSN)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, `
		DROP TABLE IF EXISTS http_pg_test;
		CREATE TABLE http_pg_test (id SERIAL PRIMARY KEY, name TEXT, value INT);
		INSERT INTO http_pg_test (name, value) VALUES ('test1', 1);
	`)
	if err != nil {
		t.Fatalf("Failed to setup test data: %v", err)
	}

	// Start HTTP server
	handler, _ := httphandler.NewHandler(pool, encryptionKey)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api/v1")
	api.GET("/health", handler.HealthCheck)
	api.POST("/session", handler.CreateSession)
	api.POST("/query", handler.HandleQuery)
	api.DELETE("/session/:session_id", handler.CloseSession)

	serverPort := findFreePort()
	go r.Run(":" + serverPort)
	time.Sleep(500 * time.Millisecond)

	// Simulate a PgSQL client connection using pgproto3
	proxyPort := findFreePort()
	tcpListener, err := net.Listen("tcp", ":"+proxyPort)
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer tcpListener.Close()

	// Start mock proxy in background
	go func() {
		clientConn, _ := tcpListener.Accept()
		defer clientConn.Close()

		backend := pgproto3.NewBackend(clientConn, clientConn)

		// Read startup
		startupRaw, err := pgparser.ReadStartupRaw(clientConn)
		if err != nil {
			t.Logf("Read startup error: %v", err)
			return
		}
		_ = startupRaw

		// Send auth ok
		backend.Send(&pgproto3.AuthenticationOk{})
		backend.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "14.0"})
		backend.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
		backend.Send(&pgproto3.BackendKeyData{ProcessID: 1, SecretKey: []byte{0, 0, 0, 0}})
		backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		backend.Flush()

		// Read query
		msg, err := backend.Receive()
		if err != nil {
			t.Logf("Receive error: %v", err)
			return
		}
		_ = msg
	}()

	// Connect as PgSQL client
	clientConn, err := net.Dial("tcp", ":"+proxyPort)
	if err != nil {
		t.Fatalf("Dial proxy failed: %v", err)
	}
	defer clientConn.Close()

	frontend := pgproto3.NewFrontend(clientConn, clientConn)

	// Send startup message
	startup := &pgproto3.StartupMessage{
		ProtocolVersion: pgproto3.ProtocolVersion30,
		Parameters: map[string]string{
			"user":     "postgres",
			"database": "testdb",
		},
	}
	frontend.Send(startup)
	frontend.Flush()

	// Wait for auth and ready
	msgCount := 0
	for msgCount < 5 {
		msg, err := frontend.Receive()
		if err != nil {
			t.Fatalf("Receive backend message error: %v", err)
		}
		t.Logf("Received: %T", msg)
		msgCount++
		if _, ok := msg.(*pgproto3.ReadyForQuery); ok {
			break
		}
	}

	// Send a query
	query := &pgproto3.Query{String: "SELECT * FROM http_pg_test;"}
	frontend.Send(query)
	frontend.Flush()

	t.Log("Integration test setup and protocol handshake passed")
}

type pgParserResult struct {
	Columns      []string   `json:"columns"`
	Rows         [][]string `json:"rows"`
	RowsAffected int64      `json:"rows_affected"`
	Error        string     `json:"error,omitempty"`
}

func findFreePort() string {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	return port
}

func doRequest(method, url string, body []byte, sessionID string) (*http.Response, error) {
	req, err := net.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if sessionID != "" {
		req.Header.Set("X-Session-ID", sessionID)
	}
	return (&http.Client{Timeout: 5 * time.Second}).Do(req)
}
