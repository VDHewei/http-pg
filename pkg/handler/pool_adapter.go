package handler

import (
	"context"

	"github.com/http-pg/http-pg/pkg/pgpool"
)

// pgPoolAdapter 将 pgpool.PoolManager 适配为 handler.Pool 接口
// handler.Pool 接口去除了 *pgxpool.Conn 依赖，使用更抽象的签名
type pgPoolAdapter struct {
	pm *pgpool.PoolManager
}

// NewPgPoolAdapter 创建 PgSQL 连接池适配器
func NewPgPoolAdapter(pm *pgpool.PoolManager) Pool {
	return &pgPoolAdapter{pm: pm}
}

func (a *pgPoolAdapter) AcquireSession(ctx context.Context, sessionID string) error {
	_, err := a.pm.AcquireSession(ctx, sessionID)
	return err
}

func (a *pgPoolAdapter) ReleaseSession(sessionID string) {
	a.pm.ReleaseSession(sessionID)
}

func (a *pgPoolAdapter) GetStats() PoolStats {
	s := a.pm.GetStats()
	return PoolStats{
		TotalConns:    s.TotalConns,
		AcquiredConns: s.AcquiredConns,
		IdleConns:     s.IdleConns,
		MaxConns:      s.MaxConns,
	}
}

func (a *pgPoolAdapter) Close() {
	a.pm.Close()
}

func (a *pgPoolAdapter) ExecSQL(ctx context.Context, sessionID, sql string) (QueryResult, error) {
	r, err := a.pm.ExecSQL(ctx, sessionID, sql)
	if err != nil {
		return QueryResult{}, err
	}
	return QueryResult{
		Columns:      r.Columns,
		Rows:         r.Rows,
		RowsAffected: r.RowsAffected,
	}, nil
}

func (a *pgPoolAdapter) ExecCommand(ctx context.Context, sessionID, sql string) (int64, error) {
	return a.pm.ExecCommand(ctx, sessionID, sql)
}
