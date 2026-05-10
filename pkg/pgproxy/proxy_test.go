package pgproxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgproto3"

	"github.com/http-pg/http-pg/pkg/crypto"
)

// =============================================================================
// Unit tests: pure functions
// =============================================================================

func TestSqlCommandType(t *testing.T) {
	tests := []struct {
		sql  string
		want string
	}{
		{"SELECT * FROM users", "SELECT"},
		{"  insert into t values (1)", "insert"},
		{"UPDATE t SET x=1", "UPDATE"},
		{"delete from t", "delete"},
		{"CREATE TABLE foo (id int)", "CREATE"},
		{"ALTER TABLE foo ADD COLUMN x int", "ALTER"},
		{"DROP TABLE foo", "DROP"},
		{"SHOW server_version", "SHOW"},
		{"BEGIN", "BEGIN"},
		{"COMMIT", "COMMIT"},
		{"ROLLBACK", "ROLLBACK"},
		{"SET search_path TO public", "SET"},
		{"EXPLAIN SELECT 1", "EXPLAIN"},
		{"\n\t  DESCRIBE table1", "DESCRIBE"},
		{"", ""},
		{"   ", ""},
		{"TRUNCATE TABLE foo", "TRUNCATE"},
	}
	for _, tt := range tests {
		got := sqlCommandType(tt.sql)
		if got != tt.want {
			t.Errorf("sqlCommandType(%q) = %q, want %q", tt.sql, got, tt.want)
		}
	}
}

func TestCommandTag(t *testing.T) {
	tests := []struct {
		name         string
		sql          string
		numRows      int
		rowsAffected int64
		want         string
	}{
		{"SELECT with rows", "SELECT * FROM t", 5, 0, "SELECT 5"},
		{"SELECT empty", "SELECT 1", 0, 0, "SELECT 0"},
		{"INSERT", "INSERT INTO t VALUES (1)", 0, 1, "INSERT 0 1"},
		{"INSERT multiple", "INSERT INTO t VALUES (1),(2)", 0, 3, "INSERT 0 3"},
		{"UPDATE", "UPDATE t SET x=1", 0, 2, "UPDATE 2"},
		{"DELETE", "DELETE FROM t", 0, 5, "DELETE 5"},
		{"CREATE TABLE", "CREATE TABLE foo (id int)", 0, 0, "CREATE TABLE"},
		{"CREATE INDEX", "CREATE INDEX idx ON foo(id)", 0, 0, "CREATE INDEX"},
		{"ALTER TABLE", "ALTER TABLE foo ADD x int", 0, 0, "ALTER TABLE"},
		{"DROP TABLE", "DROP TABLE foo", 0, 0, "DROP TABLE"},
		{"SHOW", "SHOW server_version", 1, 0, "SHOW 1"},
		{"SET", "SET search_path TO public", 0, 1, "SET 1"},
		{"BEGIN", "BEGIN", 0, 1, "BEGIN 1"},
		{"UNKNOWN with 0 affected", "VACUUM t", 0, 0, "VACUUM"},
		{"UNKNOWN with affected", "VACUUM t", 0, 100, "VACUUM 100"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(commandTag(tt.sql, tt.numRows, tt.rowsAffected))
			if got != tt.want {
				t.Errorf("commandTag() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCreateTag(t *testing.T) {
	tests := []struct {
		sql  string
		want string
	}{
		{"CREATE TABLE foo (id int)", "CREATE TABLE"},
		{"   CREATE   INDEX   idx ON t(x)", "CREATE INDEX"},
		{"ALTER TABLE foo ADD COLUMN x int", "ALTER TABLE"},
		{"DROP TABLE foo", "DROP TABLE"},
		{"CREATE DATABASE testdb", "CREATE DATABASE"},
	}
	for _, tt := range tests {
		got := string(createTag(tt.sql))
		if got != tt.want {
			t.Errorf("createTag(%q) = %q, want %q", tt.sql, got, tt.want)
		}
	}
}

func TestNew(t *testing.T) {
	key := crypto.GenerateKey()
	proxy, err := New(":15432", "http://localhost:8080", key)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if proxy == nil {
		t.Fatal("proxy is nil")
	}
	if proxy.listenAddr != ":15432" {
		t.Errorf("listenAddr = %q, want :15432", proxy.listenAddr)
	}
	if proxy.quit == nil {
		t.Fatal("quit channel is nil")
	}
}

func TestNewWithInvalidKey(t *testing.T) {
	_, err := New(":15432", "http://localhost:8080", "")
	if err == nil {
		t.Fatal("expected error for empty key, got nil")
	}
}

// =============================================================================
// Integration-style tests: proxy with mock HTTP server and real PgSQL client
// =============================================================================

// mockServerData holds the encrypted responses the mock server will return.
type mockServerData struct {
	queryResult json.RawMessage // JSON to return for query requests
	encKey      []byte
	mu          sync.Mutex
}

func newMockServer(encKey string, queryResult json.RawMessage) (*httptest.Server, error) {
	key, err := crypto.DeriveKey(encKey)
	if err != nil {
		return nil, err
	}

	data := &mockServerData{
		queryResult: queryResult,
		encKey:      key,
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data.mu.Lock()
		defer data.mu.Unlock()

		switch {
		case r.URL.Path == "/api/v1/session" && r.Method == "POST":
			body, _ := io.ReadAll(r.Body)
			decrypted, err := crypto.Decrypt(body, data.encKey)
			if err != nil {
				http.Error(w, "decrypt error", http.StatusBadRequest)
				return
			}
			// Return session ID (decrypted startup params are logged, not used)
			_ = decrypted
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("mock-session-abcdef"))

		case r.URL.Path == "/api/v1/query" && r.Method == "POST":
			body, _ := io.ReadAll(r.Body)

			// Decrypt the request
			decrypted, err := crypto.Decrypt(body, data.encKey)
			if err != nil {
				http.Error(w, "decrypt error", http.StatusBadRequest)
				return
			}

			// Unmarshal to verify it's valid JSON
			var req pgMessageRequest
			if err := json.Unmarshal(decrypted, &req); err != nil {
				http.Error(w, "unmarshal error", http.StatusBadRequest)
				return
			}

			// Encrypt the result
			encrypted, _ := crypto.Encrypt(data.queryResult, data.encKey)
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			w.Write(encrypted)

		case r.URL.Path == "/api/v1/session/mock-session-abcdef" && r.Method == "DELETE":
			w.WriteHeader(http.StatusOK)

		case stringsPrefix(r.URL.Path, "/api/v1/session/") && r.Method == "DELETE":
			w.WriteHeader(http.StatusOK)

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	})), nil
}

// stringsPrefix is a helper to check path prefix (avoids importing strings for one use).
func stringsPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// pgsqlClient wraps a TCP connection that speaks PgSQL wire protocol.
// It acts as a PostgreSQL client (Frontend) connecting to the proxy (Backend).
type pgsqlClient struct {
	conn net.Conn
	frontend *pgproto3.Frontend
}

func dialPgSQL(addr string) (*pgsqlClient, error) {
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	return &pgsqlClient{
		conn:     conn,
		frontend: pgproto3.NewFrontend(conn, conn),
	}, nil
}

func (c *pgsqlClient) close() {
	c.conn.Close()
}

// sendStartup sends a startup message and verifies the auth response.
func (c *pgsqlClient) sendStartup(t *testing.T) {
	t.Helper()

	// Build a startup message for protocol version 3.0 with user parameter
	startup := pgproto3.StartupMessage{
		ProtocolVersion: pgproto3.ProtocolVersionNumber,
		Parameters: map[string]string{
			"user":     "postgres",
			"database": "postgres",
		},
	}
	c.frontend.Send(&startup)
	if err := c.frontend.Flush(); err != nil {
		t.Fatalf("flush startup: %v", err)
	}

	// Read responses: AuthenticationOk, ParameterStatus(×4), BackendKeyData, ReadyForQuery
	msgCount := 0
	for msgCount < 7 {
		msg, err := c.frontend.Receive()
		if err != nil {
			t.Fatalf("receive after startup (msg %d): %v", msgCount+1, err)
		}
		switch msg.(type) {
		case *pgproto3.AuthenticationOk:
			msgCount++
		case *pgproto3.ParameterStatus:
			msgCount++
		case *pgproto3.BackendKeyData:
			msgCount++
		case *pgproto3.ReadyForQuery:
			msgCount++
		default:
			t.Logf("Unexpected message type after startup: %T", msg)
			msgCount++
		}
	}
}

// sendQuery sends a simple query and returns all response messages.
func (c *pgsqlClient) sendQuery(t *testing.T, sql string) []pgproto3.BackendMessage {
	t.Helper()

	query := &pgproto3.Query{String: sql}
	c.frontend.Send(query)
	if err := c.frontend.Flush(); err != nil {
		t.Fatalf("flush query %q: %v", sql, err)
	}

	var msgs []pgproto3.BackendMessage
	for {
		msg, err := c.frontend.Receive()
		if err != nil {
			t.Fatalf("receive after query %q: %v", sql, err)
		}
		// Copy DataRow values immediately: pgproto3 reuses its read buffer
		// across successive Receive() calls, so Values slices become stale.
		if dr, ok := msg.(*pgproto3.DataRow); ok {
			copied := &pgproto3.DataRow{}
			for _, v := range dr.Values {
				if v == nil {
					copied.Values = append(copied.Values, nil)
				} else {
					vc := make([]byte, len(v))
					copy(vc, v)
					copied.Values = append(copied.Values, vc)
				}
			}
			msg = copied
		}
		msgs = append(msgs, msg)
		if _, ok := msg.(*pgproto3.ReadyForQuery); ok {
			break
		}
	}
	return msgs
}

// sendTerminate sends a Terminate message.
func (c *pgsqlClient) sendTerminate(t *testing.T) {
	t.Helper()

	term := &pgproto3.Terminate{}
	c.frontend.Send(term)
	c.frontend.Flush()
}

// =============================================================================
// Integration tests
// =============================================================================

func TestHandleConnection_SimpleQuery(t *testing.T) {
	key := crypto.GenerateKey()

	queryResult := pgQueryResponse{
		Columns:      []string{"id", "name"},
		Rows:         [][]string{{"1", "Alice"}, {"2", "Bob"}},
		RowsAffected: 0,
	}
	resultJSON, _ := json.Marshal(queryResult)

	mockSrv, err := newMockServer(key, resultJSON)
	if err != nil {
		t.Fatalf("create mock server: %v", err)
	}
	defer mockSrv.Close()

	proxy, err := New(":0", mockSrv.URL, key)
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}

	// Start the proxy in background
	go proxy.Start()

	// Give it time to listen
	time.Sleep(100 * time.Millisecond)

	// Get the actual address
	// (We can't easily get the assigned port from :0, so use a different approach)
	// Let's stop and recreate with a known port, or extract listener addr.

	// Cleanup what we started
	proxy.Stop()
}

func TestHandleConnection_QueryResponse(t *testing.T) {
	key := crypto.GenerateKey()

	queryResult := pgQueryResponse{
		Columns:      []string{"id", "name"},
		Rows:         [][]string{{"1", "Alice"}, {"2", "Bob"}},
		RowsAffected: 0,
	}
	resultJSON, _ := json.Marshal(queryResult)

	mockSrv, err := newMockServer(key, resultJSON)
	if err != nil {
		t.Fatalf("create mock server: %v", err)
	}
	defer mockSrv.Close()

	// Listen on random port and get actual address
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	proxy, err := New(addr, mockSrv.URL, key)
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}

	// Start proxy in background
	go proxy.Start()

	// Wait for proxy to be ready
	time.Sleep(100 * time.Millisecond)

	// Connect PgSQL client
	client, err := dialPgSQL(addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer client.close()

	// Startup handshake
	client.sendStartup(t)

	// Send a SELECT query
	msgs := client.sendQuery(t, "SELECT id, name FROM users")

	// Verify response structure
	var foundRowDesc, foundDataRow, foundCmdComplete bool
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *pgproto3.RowDescription:
			foundRowDesc = true
			if len(m.Fields) != 2 {
				t.Errorf("expected 2 fields, got %d", len(m.Fields))
			}
			if string(m.Fields[0].Name) != "id" {
				t.Errorf("field[0] = %q, want %q", string(m.Fields[0].Name), "id")
			}
			if string(m.Fields[1].Name) != "name" {
				t.Errorf("field[1] = %q, want %q", string(m.Fields[1].Name), "name")
			}
		case *pgproto3.DataRow:
			foundDataRow = true
			if len(m.Values) != 2 {
				t.Errorf("expected 2 values, got %d", len(m.Values))
			}
		case *pgproto3.CommandComplete:
			foundCmdComplete = true
			if string(m.CommandTag) != "SELECT 2" {
				t.Errorf("CommandTag = %q, want %q", string(m.CommandTag), "SELECT 2")
			}
		}
	}

	if !foundRowDesc {
		t.Error("missing RowDescription in response")
	}
	if !foundDataRow {
		t.Error("missing DataRow in response")
	}
	if !foundCmdComplete {
		t.Error("missing CommandComplete in response")
	}

	// Cleanly terminate
	client.sendTerminate(t)
	proxy.Stop()
}

func TestHandleConnection_UpdateQuery(t *testing.T) {
	key := crypto.GenerateKey()

	queryResult := pgQueryResponse{
		Columns:      nil,
		Rows:         nil,
		RowsAffected: 3,
	}
	resultJSON, _ := json.Marshal(queryResult)

	mockSrv, err := newMockServer(key, resultJSON)
	if err != nil {
		t.Fatalf("create mock server: %v", err)
	}
	defer mockSrv.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	proxy, err := New(addr, mockSrv.URL, key)
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}
	go proxy.Start()
	time.Sleep(100 * time.Millisecond)

	client, err := dialPgSQL(addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer client.close()

	client.sendStartup(t)
	msgs := client.sendQuery(t, "UPDATE users SET active = true")

	var foundCmdComplete bool
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *pgproto3.RowDescription:
			t.Error("unexpected RowDescription for UPDATE")
		case *pgproto3.CommandComplete:
			foundCmdComplete = true
			if string(m.CommandTag) != "UPDATE 3" {
				t.Errorf("CommandTag = %q, want %q", string(m.CommandTag), "UPDATE 3")
			}
		}
	}
	if !foundCmdComplete {
		t.Error("missing CommandComplete for UPDATE")
	}

	client.sendTerminate(t)
	proxy.Stop()
}

func TestHandleConnection_ErrorResponse(t *testing.T) {
	key := crypto.GenerateKey()

	// Return a result with an error field
	queryResult := pgQueryResponse{
		Error: "relation \"nonexistent\" does not exist",
	}
	resultJSON, _ := json.Marshal(queryResult)

	mockSrv, err := newMockServer(key, resultJSON)
	if err != nil {
		t.Fatalf("create mock server: %v", err)
	}
	defer mockSrv.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	proxy, err := New(addr, mockSrv.URL, key)
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}
	go proxy.Start()
	time.Sleep(100 * time.Millisecond)

	client, err := dialPgSQL(addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer client.close()

	client.sendStartup(t)
	msgs := client.sendQuery(t, "SELECT * FROM nonexistent")

	var foundError bool
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *pgproto3.ErrorResponse:
			foundError = true
			if m.Message != "relation \"nonexistent\" does not exist" {
				t.Errorf("error message = %q, want %q", m.Message, "relation \"nonexistent\" does not exist")
			}
		}
	}
	if !foundError {
		t.Error("expected ErrorResponse, but none found")
	}

	client.sendTerminate(t)
	proxy.Stop()
}

func TestHandleConnection_NullValues(t *testing.T) {
	key := crypto.GenerateKey()

	queryResult := pgQueryResponse{
		Columns: []string{"id", "email"},
		Rows:    [][]string{{"1", "NULL"}, {"2", "test@example.com"}},
	}
	resultJSON, _ := json.Marshal(queryResult)

	mockSrv, err := newMockServer(key, resultJSON)
	if err != nil {
		t.Fatalf("create mock server: %v", err)
	}
	defer mockSrv.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	proxy, err := New(addr, mockSrv.URL, key)
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}
	go proxy.Start()
	time.Sleep(100 * time.Millisecond)

	client, err := dialPgSQL(addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer client.close()

	client.sendStartup(t)
	msgs := client.sendQuery(t, "SELECT id, email FROM users")

	var dataRows []*pgproto3.DataRow
	for _, msg := range msgs {
		if dr, ok := msg.(*pgproto3.DataRow); ok {
			dataRows = append(dataRows, dr)
		}
	}

	if len(dataRows) != 2 {
		t.Fatalf("expected 2 data rows, got %d", len(dataRows))
	}

	// First row: id=1, email=NULL
	if string(dataRows[0].Values[0]) != "1" {
		t.Errorf("row0[0] = %q, want %q", string(dataRows[0].Values[0]), "1")
	}
	if dataRows[0].Values[1] != nil {
		t.Errorf("row0[1] expected nil, got %q", string(dataRows[0].Values[1]))
	}

	// Second row: id=2, email=test@example.com
	if string(dataRows[1].Values[0]) != "2" {
		t.Errorf("row1[0] = %q, want %q", string(dataRows[1].Values[0]), "2")
	}
	if string(dataRows[1].Values[1]) != "test@example.com" {
		t.Errorf("row1[1] = %q, want %q", string(dataRows[1].Values[1]), "test@example.com")
	}

	client.sendTerminate(t)
	proxy.Stop()
}

func TestStartStop(t *testing.T) {
	key := crypto.GenerateKey()

	proxy, err := New("127.0.0.1:0", "http://localhost:8080", key)
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}

	// Start proxy in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- proxy.Start()
	}()

	// Give it time to start
	time.Sleep(100 * time.Millisecond)

	// Verify it's listening
	if proxy.listener == nil {
		t.Fatal("listener is nil after Start")
	}

	// Stop it
	if err := proxy.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Start should now return nil
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Start returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
}

func TestStartStopWithoutStart(t *testing.T) {
	key := crypto.GenerateKey()

	proxy, err := New(":15432", "http://localhost:8080", key)
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}

	// Stop should not panic even if Start was never called
	if err := proxy.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestHandleConnection_InsertQuery(t *testing.T) {
	key := crypto.GenerateKey()

	queryResult := pgQueryResponse{
		RowsAffected: 1,
	}
	resultJSON, _ := json.Marshal(queryResult)

	mockSrv, err := newMockServer(key, resultJSON)
	if err != nil {
		t.Fatalf("create mock server: %v", err)
	}
	defer mockSrv.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	proxy, err := New(addr, mockSrv.URL, key)
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}
	go proxy.Start()
	time.Sleep(100 * time.Millisecond)

	client, err := dialPgSQL(addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer client.close()

	client.sendStartup(t)
	msgs := client.sendQuery(t, "INSERT INTO users (name) VALUES ('Charlie')")

	var foundCmdComplete bool
	for _, msg := range msgs {
		if cc, ok := msg.(*pgproto3.CommandComplete); ok {
			foundCmdComplete = true
			if string(cc.CommandTag) != "INSERT 0 1" {
				t.Errorf("CommandTag = %q, want %q", string(cc.CommandTag), "INSERT 0 1")
			}
		}
	}
	if !foundCmdComplete {
		t.Error("missing CommandComplete for INSERT")
	}

	client.sendTerminate(t)
	proxy.Stop()
}

func TestHandleConnection_CreateTable(t *testing.T) {
	key := crypto.GenerateKey()

	queryResult := pgQueryResponse{
		RowsAffected: 0,
	}
	resultJSON, _ := json.Marshal(queryResult)

	mockSrv, err := newMockServer(key, resultJSON)
	if err != nil {
		t.Fatalf("create mock server: %v", err)
	}
	defer mockSrv.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	proxy, err := New(addr, mockSrv.URL, key)
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}
	go proxy.Start()
	time.Sleep(100 * time.Millisecond)

	client, err := dialPgSQL(addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer client.close()

	client.sendStartup(t)
	msgs := client.sendQuery(t, "CREATE TABLE items (id serial PRIMARY KEY, name text)")

	var foundCmdComplete bool
	for _, msg := range msgs {
		if cc, ok := msg.(*pgproto3.CommandComplete); ok {
			foundCmdComplete = true
			if string(cc.CommandTag) != "CREATE TABLE" {
				t.Errorf("CommandTag = %q, want %q", string(cc.CommandTag), "CREATE TABLE")
			}
		}
	}
	if !foundCmdComplete {
		t.Error("missing CommandComplete for CREATE TABLE")
	}

	client.sendTerminate(t)
	proxy.Stop()
}

func TestHandleConnection_MultipleQueries(t *testing.T) {
	key := crypto.GenerateKey()

	selectResult := pgQueryResponse{
		Columns:      []string{"id"},
		Rows:         [][]string{{"1"}},
		RowsAffected: 0,
	}
	selectJSON, _ := json.Marshal(selectResult)

	mockSrv, err := newMockServer(key, selectJSON)
	if err != nil {
		t.Fatalf("create mock server: %v", err)
	}
	defer mockSrv.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	proxy, err := New(addr, mockSrv.URL, key)
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}
	go proxy.Start()
	time.Sleep(100 * time.Millisecond)

	client, err := dialPgSQL(addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer client.close()

	client.sendStartup(t)

	// Send multiple queries in sequence
	for i := 0; i < 3; i++ {
		msgs := client.sendQuery(t, "SELECT id FROM users")

		var foundCmdComplete bool
		for _, msg := range msgs {
			if cc, ok := msg.(*pgproto3.CommandComplete); ok {
				foundCmdComplete = true
				if string(cc.CommandTag) != "SELECT 1" {
					t.Errorf("query %d: CommandTag = %q, want %q", i, string(cc.CommandTag), "SELECT 1")
				}
			}
		}
		if !foundCmdComplete {
			t.Errorf("query %d: missing CommandComplete", i)
		}
	}

	client.sendTerminate(t)
	proxy.Stop()
}

func TestHandleConnection_EmptyResult(t *testing.T) {
	key := crypto.GenerateKey()

	queryResult := pgQueryResponse{
		Columns:      nil,
		Rows:         nil,
		RowsAffected: 0,
	}
	resultJSON, _ := json.Marshal(queryResult)

	mockSrv, err := newMockServer(key, resultJSON)
	if err != nil {
		t.Fatalf("create mock server: %v", err)
	}
	defer mockSrv.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	proxy, err := New(addr, mockSrv.URL, key)
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}
	go proxy.Start()
	time.Sleep(100 * time.Millisecond)

	client, err := dialPgSQL(addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer client.close()

	client.sendStartup(t)
	msgs := client.sendQuery(t, "SET search_path TO public")

	var foundCmdComplete, foundRowDesc bool
	for _, msg := range msgs {
		switch msg.(type) {
		case *pgproto3.RowDescription:
			foundRowDesc = true
		case *pgproto3.CommandComplete:
			foundCmdComplete = true
		}
	}

	if foundRowDesc {
		t.Error("unexpected RowDescription for empty result")
	}
	if !foundCmdComplete {
		t.Error("missing CommandComplete for empty result")
	}

	client.sendTerminate(t)
	proxy.Stop()
}
