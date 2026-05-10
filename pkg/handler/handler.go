package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/google/uuid"

	"github.com/http-pg/http-pg/pkg/crypto"
)

// Handler 框架无关的请求处理器，封装所有核心业务逻辑
// 不含任何 HTTP 框架依赖（Gin/Fiber），由适配器层提供 HTTP 集成
type Handler struct {
	// pgPool PgSQL 连接池
	pgPool Pool
	// mysqlPool MySQL 连接池（阶段五加入，可为 nil）
	mysqlPool Pool
	// encKey AES-256-GCM 加解密密钥（已派生后的 32 字节）
	encKey []byte
	// stmts PgSQL 扩展查询协议的语句缓存（sessionID → SQL）
	stmts map[string]string
	// mu 保护 stmts 的并发访问
	mu sync.RWMutex
}

// NewHandler 创建框架无关的处理器实例
//
// 参数:
//   - pgPool: PgSQL 连接池实现
//   - mysqlPool: MySQL 连接池实现（可为 nil，阶段五启用）
//   - encKey: AES-256-GCM 加密密钥字符串
//
// 返回:
//   - *Handler: 处理器实例
//   - error: 密钥派生失败时返回错误
func NewHandler(pgPool Pool, mysqlPool Pool, encKey string) (*Handler, error) {
	key, err := crypto.DeriveKey(encKey)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}

	return &Handler{
		pgPool:    pgPool,
		mysqlPool: mysqlPool,
		encKey:    key,
		stmts:     make(map[string]string),
	}, nil
}

// CreateSession 创建新的数据库会话
//
// 处理流程:
//  1. 解密请求体中的连接参数
//  2. 生成 UUID 作为会话标识符
//  3. 根据协议类型从对应池中获取连接
//
// 参数:
//   - ctx: 请求上下文，用于超时控制和取消
//   - encryptedBody: 加密的请求体（目前为任意字节，未来可结构化）
//   - protocol: 数据库协议类型
//
// 返回:
//   - string: 生成的会话 UUID
//   - error: 解密失败或池获取连接失败时返回
func (h *Handler) CreateSession(ctx context.Context, encryptedBody []byte, protocol ProtocolType) (string, error) {
	// 解密会话参数（当前版本解密后丢弃，为后续扩展预留）
	_, err := crypto.Decrypt(encryptedBody, h.encKey)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	sessionID := uuid.New().String()

	// 根据协议类型选择对应的连接池
	pool := h.poolForProtocol(protocol)
	if pool == nil {
		return "", fmt.Errorf("unsupported protocol: %s", protocol)
	}

	if err := pool.AcquireSession(ctx, sessionID); err != nil {
		return "", fmt.Errorf("acquire connection: %w", err)
	}

	log.Printf("[Session %s] Created (%s)", sessionID[:8], protocol)
	return sessionID, nil
}

// HandleQuery 处理代理发来的 SQL 查询请求
//
// 处理流程:
//  1. 解密请求体
//  2. 反序列化 QueryRequest JSON
//  3. 处理扩展查询协议（PgSQL 的 Parse/Bind/Execute）
//  4. 根据 SQL 命令类型路由到查询或命令执行方法
//  5. 加密响应结果
//
// 参数:
//   - ctx: 请求上下文
//   - sessionID: 客户端会话标识符
//   - encryptedBody: 加密的请求体（QueryRequest 的 JSON 表示）
//
// 返回:
//   - []byte: 加密的响应体（QueryResponse 的 JSON 表示）
//   - error: 处理过程中的任何错误
func (h *Handler) HandleQuery(ctx context.Context, sessionID string, encryptedBody []byte) ([]byte, error) {
	// 第一步：解密请求体
	decrypted, err := crypto.Decrypt(encryptedBody, h.encKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	// 第二步：反序列化查询请求
	var req QueryRequest
	if err := json.Unmarshal(decrypted, &req); err != nil {
		return nil, fmt.Errorf("unmarshal request: %w", err)
	}

	log.Printf("[Session %s] SQL: %s", sessionID[:8], req.SQL)

	// 第三步：处理 PgSQL 扩展查询协议（Parse → Bind → Execute）
	// Parse('P'): 缓存 SQL 语句，返回空结果表示解析成功
	if req.Type == 'P' {
		h.mu.Lock()
		h.stmts[sessionID] = req.SQL
		h.mu.Unlock()

		responseJSON, _ := json.Marshal(QueryResponse{})
		encrypted, err := crypto.Encrypt(responseJSON, h.encKey)
		if err != nil {
			return nil, fmt.Errorf("encrypt parse response: %w", err)
		}
		return encrypted, nil
	}

	// Bind('B') / Execute('E'): 使用缓存语句
	if req.Type == 'E' || req.Type == 'B' {
		h.mu.RLock()
		cachedSQL, ok := h.stmts[sessionID]
		h.mu.RUnlock()

		if !ok || cachedSQL == "" {
			return nil, fmt.Errorf("no parsed statement found for session")
		}
		req.SQL = cachedSQL
	}

	// 第四步：确定 SQL 命令类型并执行
	cmdType := sqlCommandType(req.SQL)
	response, err := h.executeSQL(ctx, sessionID, req.SQL, cmdType)
	if err != nil {
		// 即使执行出错，也封装为正常响应（错误信息在 response 中）
		response = &QueryResponse{Error: err.Error()}
	}

	// 第五步：序列化并加密响应
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("marshal response: %w", err)
	}

	encrypted, err := crypto.Encrypt(responseJSON, h.encKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt response: %w", err)
	}

	return encrypted, nil
}

// executeSQL 根据 SQL 命令类型路由到对应的执行方法
func (h *Handler) executeSQL(ctx context.Context, sessionID, sql, cmdType string) (*QueryResponse, error) {
	switch cmdType {
	case "SELECT", "SHOW", "EXPLAIN", "DESCRIBE", "WITH":
		return h.executeQuery(ctx, sessionID, sql)
	case "INSERT", "UPDATE", "DELETE", "CREATE", "ALTER", "DROP", "TRUNCATE",
		"SET", "BEGIN", "COMMIT", "ROLLBACK", "GRANT", "REVOKE", "COPY", "VACUUM":
		return h.executeCommand(ctx, sessionID, sql)
	default:
		// 未知命令类型默认为查询
		return h.executeQuery(ctx, sessionID, sql)
	}
}

// executeQuery 执行查询类 SQL（SELECT/SHOW 等），返回完整结果集
func (h *Handler) executeQuery(ctx context.Context, sessionID, sql string) (*QueryResponse, error) {
	result, err := h.pgPool.ExecSQL(ctx, sessionID, sql)
	if err != nil {
		return nil, err
	}
	return &QueryResponse{
		Columns:      result.Columns,
		Rows:         result.Rows,
		RowsAffected: result.RowsAffected,
	}, nil
}

// executeCommand 执行命令类 SQL（INSERT/UPDATE/DELETE 等），返回影响行数
func (h *Handler) executeCommand(ctx context.Context, sessionID, sql string) (*QueryResponse, error) {
	rowsAffected, err := h.pgPool.ExecCommand(ctx, sessionID, sql)
	if err != nil {
		return nil, err
	}
	return &QueryResponse{
		RowsAffected: rowsAffected,
	}, nil
}

// CloseSession 关闭数据库会话并释放连接
//
// 参数:
//   - ctx: 请求上下文
//   - sessionID: 要关闭的会话标识符
func (h *Handler) CloseSession(ctx context.Context, sessionID string) {
	// 尝试释放两种池中的会话（幂等操作）
	h.pgPool.ReleaseSession(sessionID)
	if h.mysqlPool != nil {
		h.mysqlPool.ReleaseSession(sessionID)
	}

	// 清理扩展查询协议缓存
	h.mu.Lock()
	delete(h.stmts, sessionID)
	h.mu.Unlock()

	log.Printf("[Session %s] Closed", sessionID[:8])
}

// HealthCheck 返回系统及连接池健康状态
func (h *Handler) HealthCheck() *HealthStatus {
	status := &HealthStatus{
		Status: "healthy",
		PgPool: h.pgPool.GetStats(),
	}
	if h.mysqlPool != nil {
		status.MyPool = h.mysqlPool.GetStats()
	}
	return status
}

// poolForProtocol 根据协议类型返回对应的连接池
func (h *Handler) poolForProtocol(protocol ProtocolType) Pool {
	switch protocol {
	case ProtocolPgSQL:
		return h.pgPool
	case ProtocolMySQL:
		return h.mysqlPool
	default:
		return nil
	}
}

// sqlCommandType 提取 SQL 语句的首个命令词（SELECT/INSERT/UPDATE 等）
// 用于判断 SQL 应作为查询还是命令处理
func sqlCommandType(sql string) string {
	// 跳过前导空白字符
	i := 0
	for i < len(sql) && (sql[i] == ' ' || sql[i] == '\t' || sql[i] == '\n' || sql[i] == '\r') {
		i++
	}
	// 提取首个单词（遇到空格、括号、分号等终止）
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
