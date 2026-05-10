package pgpool

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolManager manages PgSQL connection pool for session-based connections.
type PoolManager struct {
	pool     *pgxpool.Pool
	sessions map[string]*pgxpool.Conn
	mu       sync.RWMutex
}

// New creates a new PoolManager with the given connection string.
func New(ctx context.Context, dsn string, maxConns, minConns int32) (*PoolManager, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}

	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	if minConns > 0 {
		cfg.MinConns = minConns
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &PoolManager{
		pool:     pool,
		sessions: make(map[string]*pgxpool.Conn),
	}, nil
}

// AcquireSession acquires a connection for a session.
func (pm *PoolManager) AcquireSession(ctx context.Context, sessionID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.sessions[sessionID]; exists {
		return nil
	}

	conn, err := pm.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}

	pm.sessions[sessionID] = conn
	return nil
}

// ReleaseSession releases the connection for a session.
func (pm *PoolManager) ReleaseSession(sessionID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if conn, exists := pm.sessions[sessionID]; exists {
		conn.Release()
		delete(pm.sessions, sessionID)
	}
}

// GetSessionConn returns the connection for a session.
func (pm *PoolManager) GetSessionConn(sessionID string) (*pgxpool.Conn, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	conn, exists := pm.sessions[sessionID]
	return conn, exists
}

// Stats returns pool statistics.
func (pm *PoolManager) Stats() *pgxpool.Stat {
	return pm.pool.Stat()
}

// GetStats returns pool statistics as a simple struct.
func (pm *PoolManager) GetStats() PoolStats {
	s := pm.pool.Stat()
	return PoolStats{
		TotalConns:    s.TotalConns(),
		AcquiredConns: s.AcquiredConns(),
		IdleConns:     s.IdleConns(),
		MaxConns:      s.MaxConns(),
	}
}

// Close closes all sessions and the pool.
func (pm *PoolManager) Close() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for id, conn := range pm.sessions {
		conn.Release()
		delete(pm.sessions, id)
	}
	pm.pool.Close()
}

// ExecSQL executes a SQL query on a session's connection.
func (pm *PoolManager) ExecSQL(ctx context.Context, sessionID, sql string) (QueryResult, error) {
	conn, ok := pm.GetSessionConn(sessionID)
	if !ok {
		return QueryResult{}, fmt.Errorf("session %s not found", sessionID)
	}

	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return QueryResult{}, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	result := QueryResult{
		Columns: make([]string, 0),
		Rows:    make([][]string, 0),
	}

	// Get column descriptions
	descriptions := rows.FieldDescriptions()
	for _, desc := range descriptions {
		result.Columns = append(result.Columns, string(desc.Name))
	}

	// Collect all rows
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return result, fmt.Errorf("scan row: %w", err)
		}
		row := make([]string, len(values))
		for i, v := range values {
			if v == nil {
				row[i] = "NULL"
			} else {
				row[i] = fmt.Sprintf("%v", v)
			}
		}
		result.Rows = append(result.Rows, row)
	}

	if rows.Err() != nil {
		return result, fmt.Errorf("rows iteration: %w", rows.Err())
	}

	return result, nil
}

// PoolStats holds pool health statistics.
type PoolStats struct {
	TotalConns    int32
	AcquiredConns int32
	IdleConns     int32
	MaxConns      int32
}

// QueryResult holds the result of a SQL query.
type QueryResult struct {
	Columns      []string   `json:"columns"`
	Rows         [][]string `json:"rows"`
	RowsAffected int64      `json:"rows_affected"`
}

// ExecCommand executes a SQL command (INSERT/UPDATE/DELETE/etc) on a session's connection.
func (pm *PoolManager) ExecCommand(ctx context.Context, sessionID, sql string) (int64, error) {
	conn, ok := pm.GetSessionConn(sessionID)
	if !ok {
		return 0, fmt.Errorf("session %s not found", sessionID)
	}

	tag, err := conn.Exec(ctx, sql)
	if err != nil {
		return 0, fmt.Errorf("exec failed: %w", err)
	}

	return tag.RowsAffected(), nil
}
