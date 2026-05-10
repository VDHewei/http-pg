package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/VDHewei/http-pg/pkg/crypto"
)

// =============================================================================
// Mock Pool 实现
// =============================================================================

// mockPool 模拟数据库连接池，用于测试 Handler 核心逻辑
type mockPool struct {
	mu       sync.Mutex
	sessions map[string]bool
	// execSQLFunc 自定义 ExecSQL 行为，nil 则使用默认
	execSQLFunc func(ctx context.Context, sessionID, sql string) (QueryResult, error)
	// execCmdFunc 自定义 ExecCommand 行为，nil 则使用默认
	execCmdFunc func(ctx context.Context, sessionID, sql string) (int64, error)
	// acquiredCount 记录 AcquireSession 调用次数
	acquiredCount int
}

func newMockPool() *mockPool {
	return &mockPool{
		sessions: make(map[string]bool),
	}
}

func (m *mockPool) AcquireSession(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sessionID] = true
	m.acquiredCount++
	return nil
}

func (m *mockPool) ReleaseSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

func (m *mockPool) GetStats() PoolStats {
	return PoolStats{
		TotalConns:    5,
		AcquiredConns: 2,
		IdleConns:     3,
		MaxConns:      20,
	}
}

func (m *mockPool) Close() {}

func (m *mockPool) ExecSQL(ctx context.Context, sessionID, sql string) (QueryResult, error) {
	if m.execSQLFunc != nil {
		return m.execSQLFunc(ctx, sessionID, sql)
	}
	// 默认返回固定的查询结果
	return QueryResult{
		Columns:      []string{"id", "name"},
		Rows:         [][]string{{"1", "Alice"}},
		RowsAffected: 1,
	}, nil
}

func (m *mockPool) ExecCommand(ctx context.Context, sessionID, sql string) (int64, error) {
	if m.execCmdFunc != nil {
		return m.execCmdFunc(ctx, sessionID, sql)
	}
	// 默认返回影响 1 行
	return 1, nil
}

func (m *mockPool) isSessionActive(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[sessionID]
}

// =============================================================================
// 测试辅助函数
// =============================================================================

// newTestHandler 创建用于测试的 Handler 实例
func newTestHandler(t *testing.T, pgPool Pool) *Handler {
	t.Helper()
	key := crypto.GenerateKey()
	h, err := NewHandler(pgPool, nil, key)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}
	return h
}

// encryptAndHandleQuery 加密 SQL 查询请求并调用 HandleQuery，返回解密后的响应
func encryptAndHandleQuery(t *testing.T, h *Handler, sessionID, sql string, msgType byte) *QueryResponse {
	t.Helper()

	// 构造查询请求
	req := QueryRequest{
		Type: msgType,
		SQL:  sql,
	}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	// 加密请求
	encrypted, err := crypto.Encrypt(reqJSON, h.encKey)
	if err != nil {
		t.Fatalf("encrypt request: %v", err)
	}

	// 调用 HandleQuery
	respBytes, err := h.HandleQuery(context.Background(), sessionID, encrypted)
	if err != nil {
		t.Fatalf("HandleQuery error: %v", err)
	}

	// 解密响应
	decrypted, err := crypto.Decrypt(respBytes, h.encKey)
	if err != nil {
		t.Fatalf("decrypt response: %v", err)
	}

	var resp QueryResponse
	if err := json.Unmarshal(decrypted, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	return &resp
}

// =============================================================================
// 会话管理测试
// =============================================================================

func TestHandler_CreateSession(t *testing.T) {
	h := newTestHandler(t, newMockPool())

	// 加密任意参数
	encParams, _ := crypto.Encrypt([]byte("session params"), h.encKey)

	sessionID, err := h.CreateSession(context.Background(), encParams, ProtocolPgSQL)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if sessionID == "" {
		t.Error("expected non-empty session ID")
	}
	if len(sessionID) != 36 {
		t.Errorf("expected UUID (36 chars), got %d chars: %s", len(sessionID), sessionID)
	}
}

func TestHandler_CreateSession_MultipleSessions(t *testing.T) {
	pool := newMockPool()
	h := newTestHandler(t, pool)

	encParams, _ := crypto.Encrypt([]byte("params"), h.encKey)

	// 创建多个会话，验证返回不同的 UUID
	sessions := make(map[string]bool)
	for i := 0; i < 5; i++ {
		sessionID, err := h.CreateSession(context.Background(), encParams, ProtocolPgSQL)
		if err != nil {
			t.Fatalf("CreateSession #%d failed: %v", i, err)
		}
		if sessions[sessionID] {
			t.Errorf("duplicate session ID: %s", sessionID)
		}
		sessions[sessionID] = true
	}
}

func TestHandler_CreateSession_UnsupportedProtocol(t *testing.T) {
	h := newTestHandler(t, newMockPool())

	encParams, _ := crypto.Encrypt([]byte("params"), h.encKey)

	_, err := h.CreateSession(context.Background(), encParams, ProtocolMySQL)
	if err == nil {
		t.Error("expected error for unsupported protocol")
	}
}

func TestHandler_CloseSession(t *testing.T) {
	pool := newMockPool()
	h := newTestHandler(t, pool)

	encParams, _ := crypto.Encrypt([]byte("params"), h.encKey)
	sessionID, _ := h.CreateSession(context.Background(), encParams, ProtocolPgSQL)

	if !pool.isSessionActive(sessionID) {
		t.Error("session should be active after creation")
	}

	h.CloseSession(context.Background(), sessionID)

	if pool.isSessionActive(sessionID) {
		t.Error("session should be released after close")
	}
}

func TestHandler_CloseSession_Idempotent(t *testing.T) {
	pool := newMockPool()
	h := newTestHandler(t, pool)

	sessionID := "test-session-123"

	// 关闭不存在的会话不应 panic
	h.CloseSession(context.Background(), sessionID)

	// 重复关闭也不应 panic
	h.CloseSession(context.Background(), sessionID)
}

// =============================================================================
// SQL 查询类型测试
// =============================================================================

func TestHandler_HandleQuery_SELECT(t *testing.T) {
	h := newTestHandler(t, newMockPool())

	resp := encryptAndHandleQuery(t, h, "test-session", "SELECT id, name FROM users", 'Q')

	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
	if len(resp.Columns) != 2 || resp.Columns[0] != "id" || resp.Columns[1] != "name" {
		t.Errorf("unexpected columns: %v", resp.Columns)
	}
	if len(resp.Rows) != 1 || resp.Rows[0][0] != "1" || resp.Rows[0][1] != "Alice" {
		t.Errorf("unexpected rows: %v", resp.Rows)
	}
}

func TestHandler_HandleQuery_SELECT_MultiRow(t *testing.T) {
	pool := newMockPool()
	pool.execSQLFunc = func(ctx context.Context, sessionID, sql string) (QueryResult, error) {
		return QueryResult{
			Columns: []string{"id", "name"},
			Rows: [][]string{
				{"1", "Alice"},
				{"2", "Bob"},
				{"3", "Charlie"},
			},
			RowsAffected: 3,
		}, nil
	}
	h := newTestHandler(t, pool)

	resp := encryptAndHandleQuery(t, h, "test-session", "SELECT * FROM users", 'Q')

	if len(resp.Rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(resp.Rows))
	}
	if resp.Rows[2][1] != "Charlie" {
		t.Errorf("expected 'Charlie', got %q", resp.Rows[2][1])
	}
}

func TestHandler_HandleQuery_SELECT_EmptyResult(t *testing.T) {
	pool := newMockPool()
	pool.execSQLFunc = func(ctx context.Context, sessionID, sql string) (QueryResult, error) {
		return QueryResult{
			Columns:      []string{"id", "name"},
			Rows:         [][]string{},
			RowsAffected: 0,
		}, nil
	}
	h := newTestHandler(t, pool)

	resp := encryptAndHandleQuery(t, h, "test-session", "SELECT * FROM users WHERE id = 999", 'Q')

	if len(resp.Rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(resp.Rows))
	}
	if len(resp.Columns) != 2 {
		t.Errorf("expected 2 columns, got %d", len(resp.Columns))
	}
}

func TestHandler_HandleQuery_INSERT(t *testing.T) {
	pool := newMockPool()
	pool.execCmdFunc = func(ctx context.Context, sessionID, sql string) (int64, error) {
		return 1, nil
	}
	h := newTestHandler(t, pool)

	resp := encryptAndHandleQuery(t, h, "test-session", "INSERT INTO users (name) VALUES ('Alice')", 'Q')

	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
	if resp.RowsAffected != 1 {
		t.Errorf("expected rows_affected=1, got %d", resp.RowsAffected)
	}
}

func TestHandler_HandleQuery_INSERT_MultiRow(t *testing.T) {
	pool := newMockPool()
	pool.execCmdFunc = func(ctx context.Context, sessionID, sql string) (int64, error) {
		return 3, nil
	}
	h := newTestHandler(t, pool)

	resp := encryptAndHandleQuery(t, h, "test-session", "INSERT INTO users (name) VALUES ('A'),('B'),('C')", 'Q')

	if resp.RowsAffected != 3 {
		t.Errorf("expected rows_affected=3, got %d", resp.RowsAffected)
	}
}

func TestHandler_HandleQuery_UPDATE(t *testing.T) {
	pool := newMockPool()
	pool.execCmdFunc = func(ctx context.Context, sessionID, sql string) (int64, error) {
		return 5, nil
	}
	h := newTestHandler(t, pool)

	resp := encryptAndHandleQuery(t, h, "test-session", "UPDATE users SET active = true", 'Q')

	if resp.RowsAffected != 5 {
		t.Errorf("expected rows_affected=5, got %d", resp.RowsAffected)
	}
}

func TestHandler_HandleQuery_UPDATE_ZERO(t *testing.T) {
	pool := newMockPool()
	pool.execCmdFunc = func(ctx context.Context, sessionID, sql string) (int64, error) {
		return 0, nil
	}
	h := newTestHandler(t, pool)

	resp := encryptAndHandleQuery(t, h, "test-session", "UPDATE users SET active = true WHERE id = -1", 'Q')

	if resp.RowsAffected != 0 {
		t.Errorf("expected rows_affected=0, got %d", resp.RowsAffected)
	}
}

func TestHandler_HandleQuery_DELETE(t *testing.T) {
	pool := newMockPool()
	pool.execCmdFunc = func(ctx context.Context, sessionID, sql string) (int64, error) {
		return 10, nil
	}
	h := newTestHandler(t, pool)

	resp := encryptAndHandleQuery(t, h, "test-session", "DELETE FROM users WHERE active = false", 'Q')

	if resp.RowsAffected != 10 {
		t.Errorf("expected rows_affected=10, got %d", resp.RowsAffected)
	}
}

// =============================================================================
// DDL 测试
// =============================================================================

func TestHandler_HandleQuery_CREATE_TABLE(t *testing.T) {
	h := newTestHandler(t, newMockPool())

	resp := encryptAndHandleQuery(t, h, "test-session",
		"CREATE TABLE items (id serial PRIMARY KEY, name text)", 'Q')

	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

func TestHandler_HandleQuery_CREATE_INDEX(t *testing.T) {
	h := newTestHandler(t, newMockPool())

	resp := encryptAndHandleQuery(t, h, "test-session",
		"CREATE INDEX idx_items_name ON items (name)", 'Q')

	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

func TestHandler_HandleQuery_ALTER_TABLE(t *testing.T) {
	h := newTestHandler(t, newMockPool())

	resp := encryptAndHandleQuery(t, h, "test-session",
		"ALTER TABLE items ADD COLUMN price numeric", 'Q')

	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

func TestHandler_HandleQuery_DROP_TABLE(t *testing.T) {
	h := newTestHandler(t, newMockPool())

	resp := encryptAndHandleQuery(t, h, "test-session",
		"DROP TABLE IF EXISTS old_items", 'Q')

	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

func TestHandler_HandleQuery_TRUNCATE(t *testing.T) {
	h := newTestHandler(t, newMockPool())

	resp := encryptAndHandleQuery(t, h, "test-session",
		"TRUNCATE TABLE temp_data", 'Q')

	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

// =============================================================================
// 事务测试
// =============================================================================

func TestHandler_HandleQuery_BEGIN(t *testing.T) {
	h := newTestHandler(t, newMockPool())

	resp := encryptAndHandleQuery(t, h, "test-session", "BEGIN", 'Q')

	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

func TestHandler_HandleQuery_COMMIT(t *testing.T) {
	h := newTestHandler(t, newMockPool())

	resp := encryptAndHandleQuery(t, h, "test-session", "COMMIT", 'Q')

	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

func TestHandler_HandleQuery_ROLLBACK(t *testing.T) {
	h := newTestHandler(t, newMockPool())

	resp := encryptAndHandleQuery(t, h, "test-session", "ROLLBACK", 'Q')

	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

// =============================================================================
// 配置命令测试
// =============================================================================

func TestHandler_HandleQuery_SET(t *testing.T) {
	pool := newMockPool()
	pool.execCmdFunc = func(ctx context.Context, sessionID, sql string) (int64, error) {
		return 0, nil
	}
	h := newTestHandler(t, pool)

	resp := encryptAndHandleQuery(t, h, "test-session", "SET search_path TO public", 'Q')

	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
	if resp.RowsAffected != 0 {
		t.Errorf("expected rows_affected=0 for SET, got %d", resp.RowsAffected)
	}
}

func TestHandler_HandleQuery_SHOW(t *testing.T) {
	pool := newMockPool()
	pool.execSQLFunc = func(ctx context.Context, sessionID, sql string) (QueryResult, error) {
		return QueryResult{
			Columns:      []string{"search_path"},
			Rows:         [][]string{{"public"}},
			RowsAffected: 1,
		}, nil
	}
	h := newTestHandler(t, pool)

	resp := encryptAndHandleQuery(t, h, "test-session", "SHOW search_path", 'Q')

	if len(resp.Rows) != 1 || resp.Rows[0][0] != "public" {
		t.Errorf("unexpected result: %v", resp.Rows)
	}
}

// =============================================================================
// NULL 值测试
// =============================================================================

func TestHandler_HandleQuery_NULL_Values(t *testing.T) {
	pool := newMockPool()
	pool.execSQLFunc = func(ctx context.Context, sessionID, sql string) (QueryResult, error) {
		return QueryResult{
			Columns: []string{"id", "email", "deleted_at"},
			Rows: [][]string{
				{"1", "alice@example.com", "NULL"},
				{"2", "NULL", "2024-01-01"},
			},
			RowsAffected: 2,
		}, nil
	}
	h := newTestHandler(t, pool)

	resp := encryptAndHandleQuery(t, h, "test-session", "SELECT id, email, deleted_at FROM users", 'Q')

	if len(resp.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(resp.Rows))
	}
	// NULL 值以字符串 "NULL" 表示（与 pgpool 保持一致）
	if resp.Rows[0][2] != "NULL" {
		t.Errorf("expected 'NULL' for deleted_at, got %q", resp.Rows[0][2])
	}
	if resp.Rows[1][1] != "NULL" {
		t.Errorf("expected 'NULL' for email, got %q", resp.Rows[1][1])
	}
}

// =============================================================================
// 扩展查询协议测试（PgSQL Parse/Bind/Execute）
// =============================================================================

func TestHandler_HandleQuery_ParseAndExecute(t *testing.T) {
	pool := newMockPool()
	pool.execSQLFunc = func(ctx context.Context, sessionID, sql string) (QueryResult, error) {
		if sql != "SELECT 1" {
			t.Errorf("expected cached SQL 'SELECT 1', got %q", sql)
		}
		return QueryResult{
			Columns:      []string{"?column?"},
			Rows:         [][]string{{"1"}},
			RowsAffected: 1,
		}, nil
	}
	h := newTestHandler(t, pool)

	// 第一步：Parse（类型 'P'）缓存 SQL
	respParse := encryptAndHandleQuery(t, h, "test-session", "SELECT 1", 'P')
	if respParse.Error != "" {
		t.Errorf("Parse failed: %s", respParse.Error)
	}

	// 第二步：Execute（类型 'E'）使用缓存的 SQL
	respExec := encryptAndHandleQuery(t, h, "test-session", "", 'E')
	if respExec.Error != "" {
		t.Errorf("Execute failed: %s", respExec.Error)
	}
	if len(respExec.Rows) != 1 || respExec.Rows[0][0] != "1" {
		t.Errorf("unexpected Execute result: %v", respExec.Rows)
	}
}

func TestHandler_HandleQuery_Execute_NoCachedStatement(t *testing.T) {
	h := newTestHandler(t, newMockPool())

	// 没有先 Parse 就 Execute，应该出错
	req := QueryRequest{Type: 'E', SQL: ""}
	reqJSON, _ := json.Marshal(req)
	encrypted, _ := crypto.Encrypt(reqJSON, h.encKey)

	_, err := h.HandleQuery(context.Background(), "test-session", encrypted)
	if err == nil {
		t.Error("expected error for Execute without Parse")
	}
}

func TestHandler_HandleQuery_Bind(t *testing.T) {
	pool := newMockPool()
	pool.execSQLFunc = func(ctx context.Context, sessionID, sql string) (QueryResult, error) {
		if sql != "SELECT $1" {
			t.Errorf("expected cached SQL 'SELECT $1', got %q", sql)
		}
		return QueryResult{
			Columns:      []string{"?column?"},
			Rows:         [][]string{{"42"}},
			RowsAffected: 1,
		}, nil
	}
	h := newTestHandler(t, pool)

	// Parse 缓存 SQL
	encryptAndHandleQuery(t, h, "test-session", "SELECT $1", 'P')

	// Bind（类型 'B'）使用缓存的 SQL
	respBind := encryptAndHandleQuery(t, h, "test-session", "", 'B')
	if respBind.Error != "" {
		t.Errorf("Bind failed: %s", respBind.Error)
	}
}

// =============================================================================
// 错误场景测试
// =============================================================================

func TestHandler_HandleQuery_SyntaxError(t *testing.T) {
	pool := newMockPool()
	pool.execSQLFunc = func(ctx context.Context, sessionID, sql string) (QueryResult, error) {
		return QueryResult{}, fmt.Errorf("syntax error at or near \"SLECT\"")
	}
	h := newTestHandler(t, pool)

	resp := encryptAndHandleQuery(t, h, "test-session", "SLECT * FROM users", 'Q')

	if resp.Error == "" {
		t.Error("expected error for invalid SQL")
	}
}

func TestHandler_HandleQuery_PoolError(t *testing.T) {
	pool := newMockPool()
	pool.execSQLFunc = func(ctx context.Context, sessionID, sql string) (QueryResult, error) {
		return QueryResult{}, fmt.Errorf("connection refused")
	}
	h := newTestHandler(t, pool)

	resp := encryptAndHandleQuery(t, h, "test-session", "SELECT * FROM users", 'Q')

	if resp.Error == "" {
		t.Error("expected error for pool failure")
	}
}

func TestHandler_HandleQuery_DecryptError(t *testing.T) {
	h := newTestHandler(t, newMockPool())

	// 发送未加密的原始数据
	_, err := h.HandleQuery(context.Background(), "test-session", []byte("raw unencrypted data"))
	if err == nil {
		t.Error("expected error for unencrypted input")
	}
}

func TestHandler_HandleQuery_InvalidJSON(t *testing.T) {
	h := newTestHandler(t, newMockPool())

	// 发送加密后的无效 JSON
	invalidJSON, _ := crypto.Encrypt([]byte("{not valid json"), h.encKey)
	_, err := h.HandleQuery(context.Background(), "test-session", invalidJSON)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestHandler_HandleQuery_MultipleQueries(t *testing.T) {
	pool := newMockPool()
	callCount := 0
	pool.execSQLFunc = func(ctx context.Context, sessionID, sql string) (QueryResult, error) {
		callCount++
		return QueryResult{
			Columns:      []string{"id"},
			Rows:         [][]string{{fmt.Sprintf("%d", callCount)}},
			RowsAffected: 1,
		}, nil
	}
	h := newTestHandler(t, pool)

	// 同一会话执行多次查询
	for i := 1; i <= 5; i++ {
		resp := encryptAndHandleQuery(t, h, "test-session", "SELECT id FROM users", 'Q')
		if resp.Error != "" {
			t.Errorf("query #%d failed: %s", i, resp.Error)
		}
	}

	if callCount != 5 {
		t.Errorf("expected 5 ExecSQL calls, got %d", callCount)
	}
}

// =============================================================================
// 健康检查测试
// =============================================================================

func TestHandler_HealthCheck(t *testing.T) {
	h := newTestHandler(t, newMockPool())

	status := h.HealthCheck()

	if status.Status != "healthy" {
		t.Errorf("expected status 'healthy', got %q", status.Status)
	}
	if status.PgPool.TotalConns != 5 {
		t.Errorf("expected total_conns=5, got %d", status.PgPool.TotalConns)
	}
	if status.PgPool.AcquiredConns != 2 {
		t.Errorf("expected acquired_conns=2, got %d", status.PgPool.AcquiredConns)
	}
	if status.PgPool.IdleConns != 3 {
		t.Errorf("expected idle_conns=3, got %d", status.PgPool.IdleConns)
	}
	if status.PgPool.MaxConns != 20 {
		t.Errorf("expected max_conns=20, got %d", status.PgPool.MaxConns)
	}
}

// =============================================================================
// sqlCommandType 单元测试
// =============================================================================

func TestSqlCommandType(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{"SELECT", "SELECT * FROM users", "SELECT"},
		{"select_lower", "select 1", "select"},
		{"INSERT", "INSERT INTO t VALUES (1)", "INSERT"},
		{"UPDATE", "UPDATE t SET x = 1", "UPDATE"},
		{"DELETE", "DELETE FROM t", "DELETE"},
		{"CREATE", "CREATE TABLE t (id int)", "CREATE"},
		{"ALTER", "ALTER TABLE t ADD col int", "ALTER"},
		{"DROP", "DROP TABLE t", "DROP"},
		{"TRUNCATE", "TRUNCATE TABLE t", "TRUNCATE"},
		{"SET", "SET search_path TO public", "SET"},
		{"SHOW", "SHOW search_path", "SHOW"},
		{"BEGIN", "BEGIN", "BEGIN"},
		{"COMMIT", "COMMIT", "COMMIT"},
		{"ROLLBACK", "ROLLBACK", "ROLLBACK"},
		{"EXPLAIN", "EXPLAIN SELECT * FROM users", "EXPLAIN"},
		{"WITH", "WITH cte AS (SELECT 1) SELECT * FROM cte", "WITH"},
		{"leading_whitespace", "   SELECT 1", "SELECT"},
		{"leading_tabs", "\t\tDELETE FROM t", "DELETE"},
		{"with_parenthesis", "SELECT(1)", "SELECT"},
		{"with_semicolon", "INSERT;", "INSERT"},
		{"empty", "", ""},
		{"whitespace_only", "   \t  ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sqlCommandType(tt.sql)
			if got != tt.want {
				t.Errorf("sqlCommandType(%q) = %q, want %q", tt.sql, got, tt.want)
			}
		})
	}
}

// =============================================================================
// 并发安全测试
// =============================================================================

func TestHandler_ConcurrentQueries(t *testing.T) {
	h := newTestHandler(t, newMockPool())
	sessionID := "concurrent-session"

	// 创建会话
	encParams, _ := crypto.Encrypt([]byte("params"), h.encKey)
	h.CreateSession(context.Background(), encParams, ProtocolPgSQL)

	// 并发执行查询
	const numGoroutines = 10
	errCh := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			resp := encryptAndHandleQuery(t, h, sessionID, fmt.Sprintf("SELECT %d", i), 'Q')
			if resp.Error != "" {
				errCh <- fmt.Errorf("goroutine %d error: %s", i, resp.Error)
			} else {
				errCh <- nil
			}
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		if err := <-errCh; err != nil {
			t.Error(err)
		}
	}
}

func TestHandler_ConcurrentParseAndExecute(t *testing.T) {
	h := newTestHandler(t, newMockPool())
	sessionID := "concurrent-parse"

	var wg sync.WaitGroup

	// 并发执行 Parse + Execute，验证 stmts map 的并发安全
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sql := fmt.Sprintf("SELECT %d", i)
			encryptAndHandleQuery(t, h, sessionID, sql, 'P')
			encryptAndHandleQuery(t, h, sessionID, "", 'E')
		}(i)
	}

	wg.Wait()
}

// =============================================================================
// 边界值测试
// =============================================================================

func TestHandler_HandleQuery_EmptySQL(t *testing.T) {
	h := newTestHandler(t, newMockPool())

	resp := encryptAndHandleQuery(t, h, "test-session", "", 'Q')

	// 空 SQL 被视为默认查询类型
	if resp.Error != "" {
		t.Errorf("unexpected error for empty SQL: %s", resp.Error)
	}
}

func TestHandler_HandleQuery_SpecialCharacters(t *testing.T) {
	pool := newMockPool()
	pool.execSQLFunc = func(ctx context.Context, sessionID, sql string) (QueryResult, error) {
		return QueryResult{
			Columns: []string{"result"},
			Rows: [][]string{
				{sql}, // 返回原 SQL 验证传递正确性
			},
		}, nil
	}
	h := newTestHandler(t, pool)

	sql := "SELECT * FROM users WHERE name = 'O''Brien' AND data = '\\n\\t'"
	resp := encryptAndHandleQuery(t, h, "test-session", sql, 'Q')

	if resp.Rows[0][0] != sql {
		t.Errorf("SQL not transmitted correctly: got %q", resp.Rows[0][0])
	}
}

func TestHandler_HandleQuery_LargeSQL(t *testing.T) {
	pool := newMockPool()
	pool.execSQLFunc = func(ctx context.Context, sessionID, sql string) (QueryResult, error) {
		return QueryResult{
			Columns: []string{"len"},
			Rows:    [][]string{{fmt.Sprintf("%d", len(sql))}},
		}, nil
	}
	h := newTestHandler(t, pool)

	// 构造一个 10KB 的 SQL 语句
	largeSQL := "SELECT " + makeLargeString(10000)
	resp := encryptAndHandleQuery(t, h, "test-session", largeSQL, 'Q')

	if resp.Error != "" {
		t.Errorf("unexpected error for large SQL: %s", resp.Error)
	}
}

func makeLargeString(size int) string {
	b := make([]byte, size)
	for i := range b {
		b[i] = byte('a' + (i % 26))
	}
	return string(b)
}
