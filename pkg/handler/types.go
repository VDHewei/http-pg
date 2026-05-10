package handler

import "context"

// ProtocolType 数据库协议类型
type ProtocolType string

const (
	// ProtocolPgSQL PgSQL 协议类型
	ProtocolPgSQL ProtocolType = "pg"
	// ProtocolMySQL MySQL 协议类型（阶段五使用）
	ProtocolMySQL ProtocolType = "mysql"
)

// SessionRequest 客户端创建会话请求
type SessionRequest struct {
	// Protocol 数据库协议类型（pg / mysql）
	Protocol ProtocolType
	// Params 连接参数（加密的原始字节）
	Params []byte
}

// QueryRequest 代理发来的 SQL 查询请求
// 与 pkg/pgproxy 中的 pgMessageRequest 对应
type QueryRequest struct {
	// Type PgSQL 消息类型字节，MySQL 时为命令类型
	Type byte `json:"type"`
	// SQL 提取的 SQL 语句
	SQL string `json:"sql"`
	// Raw 原始消息负载
	Raw []byte `json:"raw,omitempty"`
}

// QueryResponse SQL 查询响应，返回给代理
type QueryResponse struct {
	// Columns 结果集列名列表
	Columns []string `json:"columns,omitempty"`
	// Rows 结果集数据行（全部为字符串表示）
	Rows [][]string `json:"rows,omitempty"`
	// RowsAffected 受影响行数（DML 命令）
	RowsAffected int64 `json:"rows_affected"`
	// Error 错误信息（空表示成功）
	Error string `json:"error,omitempty"`
}

// PoolStats 连接池统计数据
type PoolStats struct {
	TotalConns    int32 `json:"total_conns"`
	AcquiredConns int32 `json:"acquired_conns"`
	IdleConns     int32 `json:"idle_conns"`
	MaxConns      int32 `json:"max_conns"`
}

// HealthStatus 服务健康状态
type HealthStatus struct {
	Status  string    `json:"status"`
	PgPool  PoolStats `json:"pg_pool"`
	MyPool  PoolStats `json:"my_pool,omitempty"`
}

// QueryResult SQL 执行结果，用于池层与 handler 间的数据传输
type QueryResult struct {
	Columns      []string   `json:"columns"`
	Rows         [][]string `json:"rows"`
	RowsAffected int64      `json:"rows_affected"`
}

// Pool 数据库连接池抽象接口
// 对每种数据库（PgSQL、MySQL）分别实现
type Pool interface {
	// AcquireSession 为 sessionID 获取一个连接
	AcquireSession(ctx context.Context, sessionID string) error
	// ReleaseSession 释放 sessionID 持有的连接
	ReleaseSession(sessionID string)
	// GetStats 返回连接池统计信息
	GetStats() PoolStats
	// Close 关闭连接池，释放所有资源
	Close()
	// ExecSQL 执行 SELECT/SHOW 等查询类 SQL，返回结果集
	ExecSQL(ctx context.Context, sessionID, sql string) (QueryResult, error)
	// ExecCommand 执行 INSERT/UPDATE/DELETE 等命令类 SQL，返回影响行数
	ExecCommand(ctx context.Context, sessionID, sql string) (int64, error)
}
