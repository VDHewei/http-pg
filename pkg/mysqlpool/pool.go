package mysqlpool

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"

	_ "github.com/go-sql-driver/mysql"

	"github.com/VDHewei/http-pg/pkg/handler"
)

// PoolManager 管理 MySQL 连接池，实现 handler.Pool 接口
// 使用 database/sql 标准库 + go-sql-driver/mysql
type PoolManager struct {
	db       *sql.DB
	sessions map[string]*sql.Conn
	mu       sync.RWMutex
	maxConns int32
}

// New 创建 MySQL 连接池管理器
// dsn 支持两种格式:
//   - MySQL URL: mysql://user:password@host:port/dbname?params
//   - 原生 DSN: user:password@tcp(host:port)/dbname?params
func New(ctx context.Context, rawDSN string, maxConns, minConns int32) (*PoolManager, error) {
	dsn, err := parseMySQLDSN(rawDSN)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(int(maxConns))
	db.SetMaxIdleConns(int(minConns))
	db.SetConnMaxLifetime(0) // 连接不过期，由连接池管理

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	pm := &PoolManager{
		db:       db,
		sessions: make(map[string]*sql.Conn),
		maxConns: maxConns,
	}

	log.Printf("MySQL pool created: max=%d min=%d", maxConns, minConns)
	return pm, nil
}

// AcquireSession 为指定会话获取一个数据库连接（幂等）
func (pm *PoolManager) AcquireSession(ctx context.Context, sessionID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.sessions[sessionID]; exists {
		return nil
	}

	conn, err := pm.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}

	pm.sessions[sessionID] = conn
	return nil
}

// ReleaseSession 释放指定会话的连接（幂等）
func (pm *PoolManager) ReleaseSession(sessionID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if conn, exists := pm.sessions[sessionID]; exists {
		conn.Close()
		delete(pm.sessions, sessionID)
	}
}

// GetStats 返回连接池统计信息
func (pm *PoolManager) GetStats() handler.PoolStats {
	if pm.db == nil {
		return handler.PoolStats{MaxConns: pm.maxConns}
	}
	stats := pm.db.Stats()
	return handler.PoolStats{
		TotalConns:    int32(stats.OpenConnections),
		AcquiredConns: int32(stats.InUse),
		IdleConns:     int32(stats.Idle),
		MaxConns:      pm.maxConns,
	}
}

// Close 关闭所有会话连接及底层连接池
func (pm *PoolManager) Close() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for id, conn := range pm.sessions {
		conn.Close()
		delete(pm.sessions, id)
	}
	pm.db.Close()
}

// ExecSQL 执行查询类 SQL（SELECT/SHOW 等），返回完整结果集
func (pm *PoolManager) ExecSQL(ctx context.Context, sessionID, sql string) (handler.QueryResult, error) {
	conn, err := pm.getSessionConn(sessionID)
	if err != nil {
		return handler.QueryResult{}, err
	}

	rows, err := conn.QueryContext(ctx, sql)
	if err != nil {
		return handler.QueryResult{}, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return handler.QueryResult{}, fmt.Errorf("get columns: %w", err)
	}

	result := handler.QueryResult{
		Columns: columns,
		Rows:    make([][]string, 0),
	}

	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return result, fmt.Errorf("scan row: %w", err)
		}

		row := make([]string, len(columns))
		for i, v := range values {
			if v == nil {
				row[i] = "NULL"
			} else {
				// 处理 []byte 类型（MySQL 可能返回字节切片）
				switch val := v.(type) {
				case []byte:
					row[i] = string(val)
				default:
					row[i] = fmt.Sprintf("%v", val)
				}
			}
		}
		result.Rows = append(result.Rows, row)
	}

	if rows.Err() != nil {
		return result, fmt.Errorf("rows iteration: %w", rows.Err())
	}

	return result, nil
}

// ExecCommand 执行命令类 SQL（INSERT/UPDATE/DELETE 等），返回影响行数
func (pm *PoolManager) ExecCommand(ctx context.Context, sessionID, sql string) (int64, error) {
	conn, err := pm.getSessionConn(sessionID)
	if err != nil {
		return 0, err
	}

	result, err := conn.ExecContext(ctx, sql)
	if err != nil {
		return 0, fmt.Errorf("exec failed: %w", err)
	}

	return result.RowsAffected()
}

// getSessionConn 获取指定会话的数据库连接（内部方法）
func (pm *PoolManager) getSessionConn(sessionID string) (*sql.Conn, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	conn, exists := pm.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return conn, nil
}

// parseMySQLDSN 将 DSN 字符串转换为 go-sql-driver/mysql 可识别的格式
//
// 支持两种输入格式:
//   - URL 格式: mysql://user:password@host:port/dbname?params
//   - 原生 DSN: user:password@tcp(host:port)/dbname?params（直接透传）
func parseMySQLDSN(dsn string) (string, error) {
	// 如果已是原生 DSN 格式（不含 mysql:// 前缀），直接返回
	if !strings.HasPrefix(dsn, "mysql://") {
		return dsn, nil
	}

	// 去掉 mysql:// 前缀
	trimmed := strings.TrimPrefix(dsn, "mysql://")

	// 解析用户信息与主机部分
	// 格式: user:password@host:port/dbname?params 或 user@host:port/dbname?params
	atIdx := strings.LastIndex(trimmed, "@")
	if atIdx == -1 {
		return "", fmt.Errorf("invalid MySQL DSN: missing '@' separator in %q", dsn)
	}

	userInfo := trimmed[:atIdx]
	hostPart := trimmed[atIdx+1:]

	// 分离数据库名和查询参数
	slashIdx := strings.Index(hostPart, "/")
	if slashIdx == -1 {
		return "", fmt.Errorf("invalid MySQL DSN: missing database name in %q", dsn)
	}

	hostAndPort := hostPart[:slashIdx]
	dbAndParams := hostPart[slashIdx+1:]

	// 分离数据库名和查询参数
	dbName := dbAndParams
	params := ""
	if qIdx := strings.Index(dbAndParams, "?"); qIdx != -1 {
		dbName = dbAndParams[:qIdx]
		params = dbAndParams[qIdx:]
	}

	// 确保端口存在（默认 3306）
	if !strings.Contains(hostAndPort, ":") {
		hostAndPort += ":3306"
	}

	// URL 解码用户信息（处理 %40 等编码）
	decodedUserInfo, err := url.QueryUnescape(userInfo)
	if err != nil {
		decodedUserInfo = userInfo
	}

	// 构建原生 DSN: user:password@tcp(host:port)/dbname?params
	nativeDSN := fmt.Sprintf("%s@tcp(%s)/%s%s", decodedUserInfo, hostAndPort, dbName, params)
	return nativeDSN, nil
}
