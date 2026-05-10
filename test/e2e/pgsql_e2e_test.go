package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/VDHewei/http-pg/pkg/handler"
	"github.com/VDHewei/http-pg/pkg/mysqlpool"
	"github.com/VDHewei/http-pg/pkg/pgpool"
	"github.com/VDHewei/http-pg/pkg/pgproxy"
)

// startPgProxy 启动 PgSQL 代理
func startPgProxy(t *testing.T, serverURL string) (*pgproxy.Proxy, string) {
	t.Helper()
	port := freePort(t)
	p, err := pgproxy.New("0.0.0.0:"+port, serverURL, encKey)
	if err != nil {
		t.Fatalf("create pg proxy: %v", err)
	}
	go func() { _ = p.Start() }()
	time.Sleep(200 * time.Millisecond)
	return p, "127.0.0.1:" + port
}

// TestPgSQLComprehensive 使用真实 PgSQL 代理执行综合 SQL 测试
//
// 链路: pgx → TCP → PgSQL Proxy → HTTP → Handler → pgpool → Docker PostgreSQL
func TestPgSQLComprehensive(t *testing.T) {
	// 1. 初始化真实连接池
	ctx := context.Background()

	pgPool, err := pgpool.New(ctx, postgresDSN, 10, 2)
	if err != nil {
		t.Fatalf("create pgpool: %v", err)
	}
	defer pgPool.Close()

	myPool, err := mysqlpool.New(ctx, mysqlDSN, 10, 2)
	if err != nil {
		t.Fatalf("create mysqlpool: %v", err)
	}
	defer myPool.Close()

	// 2. 启动 HTTP 服务器 + PgSQL 代理
	serverURL, stopServer := startServer(t, handler.NewPgPoolAdapter(pgPool), myPool)
	defer stopServer()

	proxy, proxyAddr := startPgProxy(t, serverURL)
	defer proxy.Stop()

	// 3. PgSQL 客户端连接代理
	dsn := fmt.Sprintf("postgres://postgres:postgres@%s/testdb?sslmode=disable&default_query_exec_mode=simple_protocol", proxyAddr)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to pg proxy: %v", err)
	}
	defer conn.Close(ctx)

	// ========== DDL: 创库 / 创表 ==========

	t.Run("CREATE_SCHEMA_AND_TABLE", func(t *testing.T) {
		// PostgreSQL: 新建 schema 模拟 "create database"
		_, err := conn.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS e2e_schema")
		if err != nil {
			t.Fatalf("CREATE SCHEMA failed: %v", err)
		}
		t.Log("CREATE SCHEMA e2e_schema OK")

		// 在 e2e_schema 中建表
		_, err = conn.Exec(ctx, "DROP TABLE IF EXISTS e2e_schema.e2e_users")
		if err != nil {
			t.Fatalf("DROP TABLE failed: %v", err)
		}

		_, err = conn.Exec(ctx, `
			CREATE TABLE e2e_schema.e2e_users (
				id SERIAL PRIMARY KEY,
				name VARCHAR(100) NOT NULL,
				email VARCHAR(200),
				age INT DEFAULT 0
			)
		`)
		if err != nil {
			t.Fatalf("CREATE TABLE failed: %v", err)
		}
		t.Log("CREATE TABLE e2e_schema.e2e_users OK")
	})

	// ========== DML: 插入 ==========

	t.Run("INSERT", func(t *testing.T) {
		tag, err := conn.Exec(ctx,
			"INSERT INTO e2e_schema.e2e_users (name, email, age) VALUES ('Alice', 'alice@test.com', 30)")
		if err != nil {
			t.Fatalf("INSERT Alice failed: %v", err)
		}
		t.Logf("INSERT Alice: %s", tag)

		_, err = conn.Exec(ctx,
			"INSERT INTO e2e_schema.e2e_users (name, email, age) VALUES ('Bob', 'bob@test.com', 25)")
		if err != nil {
			t.Fatalf("INSERT Bob failed: %v", err)
		}
		t.Log("INSERT Bob OK")
	})

	// ========== DML: 查询 ==========

	t.Run("SELECT", func(t *testing.T) {
		rows, err := conn.Query(ctx,
			"SELECT id, name, email, age FROM e2e_schema.e2e_users ORDER BY id")
		if err != nil {
			t.Fatalf("SELECT failed: %v", err)
		}
		defer rows.Close()

		var count int
		for rows.Next() {
			var id, age int
			var name, email string
			if err := rows.Scan(&id, &name, &email, &age); err != nil {
				t.Fatalf("scan row: %v", err)
			}
			t.Logf("  row: id=%d name=%s email=%s age=%d", id, name, email, age)
			count++
		}
		if count != 2 {
			t.Errorf("expected 2 rows, got %d", count)
		}
	})

	// ========== DML: 更新 ==========

	t.Run("UPDATE", func(t *testing.T) {
		tag, err := conn.Exec(ctx,
			"UPDATE e2e_schema.e2e_users SET name='Alice Updated', age=31 WHERE name='Alice'")
		if err != nil {
			t.Fatalf("UPDATE failed: %v", err)
		}
		t.Logf("UPDATE: %s", tag)

		var name string
		err = conn.QueryRow(ctx,
			"SELECT name FROM e2e_schema.e2e_users WHERE id=1").Scan(&name)
		if err != nil {
			t.Fatalf("SELECT after UPDATE failed: %v", err)
		}
		if name != "Alice Updated" {
			t.Errorf("expected 'Alice Updated', got %q", name)
		}
		t.Logf("Verified: name=%s", name)
	})

	// ========== DML: 删除记录 ==========

	t.Run("DELETE_RECORDS", func(t *testing.T) {
		_, err := conn.Exec(ctx,
			"INSERT INTO e2e_schema.e2e_users (name, email, age) VALUES ('Tmp', 'tmp@test.com', 1)")
		if err != nil {
			t.Fatalf("INSERT tmp failed: %v", err)
		}

		tag, err := conn.Exec(ctx,
			"DELETE FROM e2e_schema.e2e_users WHERE name='Tmp'")
		if err != nil {
			t.Fatalf("DELETE failed: %v", err)
		}
		t.Logf("DELETE: %s", tag)

		// 验证删除
		var count int
		conn.QueryRow(ctx,
			"SELECT COUNT(*) FROM e2e_schema.e2e_users WHERE name='Tmp'").Scan(&count)
		if count != 0 {
			t.Errorf("expected 0 rows with name='Tmp', got %d", count)
		}
		t.Log("Verified: Tmp row deleted")
	})

	// ========== DDL: 改表 (ALTER TABLE) ==========

	t.Run("ALTER_TABLE", func(t *testing.T) {
		_, err := conn.Exec(ctx,
			"ALTER TABLE e2e_schema.e2e_users ADD COLUMN phone VARCHAR(20)")
		if err != nil {
			t.Fatalf("ALTER TABLE ADD COLUMN failed: %v", err)
		}
		t.Log("ALTER TABLE ADD COLUMN phone OK")

		_, err = conn.Exec(ctx,
			"UPDATE e2e_schema.e2e_users SET phone='12345' WHERE id=1")
		if err != nil {
			t.Fatalf("UPDATE phone failed: %v", err)
		}

		var phone string
		err = conn.QueryRow(ctx,
			"SELECT phone FROM e2e_schema.e2e_users WHERE id=1").Scan(&phone)
		if err != nil {
			t.Fatalf("SELECT phone failed: %v", err)
		}
		if phone != "12345" {
			t.Errorf("expected phone='12345', got %q", phone)
		}
		t.Logf("Verified: phone=%s", phone)
	})

	// ========== DCL: 创建角色 / 授权 / 移除授权 ==========

	t.Run("CREATE_ROLE_GRANT_REVOKE", func(t *testing.T) {
		// 清理旧角色
		conn.Exec(ctx, "DROP ROLE IF EXISTS e2e_testuser")

		// 创建角色
		_, err := conn.Exec(ctx,
			"CREATE ROLE e2e_testuser WITH LOGIN PASSWORD 'testpass'")
		if err != nil {
			t.Fatalf("CREATE ROLE failed: %v", err)
		}
		t.Log("CREATE ROLE e2e_testuser OK")

		// 授权: 允许 SELECT
		_, err = conn.Exec(ctx,
			"GRANT SELECT ON e2e_schema.e2e_users TO e2e_testuser")
		if err != nil {
			t.Fatalf("GRANT SELECT failed: %v", err)
		}
		t.Log("GRANT SELECT OK")

		// 验证授权: 以新用户身份连接
		testUserDSN := fmt.Sprintf("postgres://e2e_testuser:testpass@%s/testdb?sslmode=disable", proxyAddr)
		testConn, err := pgx.Connect(ctx, testUserDSN)
		if err != nil {
			t.Fatalf("connect as testuser: %v", err)
		}
		defer testConn.Close(ctx)

		var count int
		err = testConn.QueryRow(ctx,
			"SELECT COUNT(*) FROM e2e_schema.e2e_users").Scan(&count)
		if err != nil {
			t.Fatalf("SELECT as testuser failed: %v", err)
		}
		t.Logf("Verified: testuser can connect, count=%d", count)

		// 移除授权
		_, err = conn.Exec(ctx,
			"REVOKE SELECT ON e2e_schema.e2e_users FROM e2e_testuser")
		if err != nil {
			t.Fatalf("REVOKE SELECT failed: %v", err)
		}
		t.Log("REVOKE SELECT OK")
	})

	// ========== 切换用户账号 (SET ROLE) ==========

	t.Run("USER_SWITCHING", func(t *testing.T) {
		conn.Exec(ctx, "DROP ROLE IF EXISTS e2e_role_a")
		conn.Exec(ctx, "DROP ROLE IF EXISTS e2e_role_b")

		// 创建两个角色
		_, err := conn.Exec(ctx, "CREATE ROLE e2e_role_a WITH LOGIN PASSWORD 'pass_a'")
		if err != nil {
			t.Fatalf("CREATE ROLE e2e_role_a failed: %v", err)
		}
		_, err = conn.Exec(ctx, "CREATE ROLE e2e_role_b WITH LOGIN PASSWORD 'pass_b'")
		if err != nil {
			t.Fatalf("CREATE ROLE e2e_role_b failed: %v", err)
		}
		// 授予 postgres 这些角色的权限
		conn.Exec(ctx, "GRANT e2e_role_a TO postgres")
		conn.Exec(ctx, "GRANT e2e_role_b TO postgres")
		conn.Exec(ctx, "GRANT USAGE ON SCHEMA e2e_schema TO e2e_role_a, e2e_role_b")
		conn.Exec(ctx, "GRANT SELECT ON e2e_schema.e2e_users TO e2e_role_a")
		conn.Exec(ctx, "GRANT SELECT ON e2e_schema.e2e_users TO e2e_role_b")

		// 切换到 e2e_role_a
		_, err = conn.Exec(ctx, "SET ROLE e2e_role_a")
		if err != nil {
			t.Fatalf("SET ROLE e2e_role_a failed: %v", err)
		}
		t.Log("Switched to e2e_role_a")

		var name string
		err = conn.QueryRow(ctx,
			"SELECT name FROM e2e_schema.e2e_users WHERE id=1").Scan(&name)
		if err != nil {
			t.Fatalf("Role A Scan failed: %v", err)
		}
		t.Logf("Role A read: name=%s", name)

		// 切换到 e2e_role_b
		_, err = conn.Exec(ctx, "SET ROLE e2e_role_b")
		if err != nil {
			t.Fatalf("SET ROLE e2e_role_b failed: %v", err)
		}
		t.Log("Switched to e2e_role_b")

		err = conn.QueryRow(ctx,
			"SELECT name FROM e2e_schema.e2e_users WHERE id=2").Scan(&name)
		if err != nil {
			t.Fatalf("Role B Scan failed: %v", err)
		}
		t.Logf("Role B read: name=%s", name)

		// 恢复原始角色
		conn.Exec(ctx, "RESET ROLE")
		t.Log("Rese to original role")

		// 清理
		conn.Exec(ctx, "REVOKE ALL ON e2e_schema.e2e_users FROM e2e_role_a, e2e_role_b")
		conn.Exec(ctx, "DROP ROLE IF EXISTS e2e_role_a")
		conn.Exec(ctx, "DROP ROLE IF EXISTS e2e_role_b")
	})

	// ========== DDL: 删表 ==========

	t.Run("DROP_TABLE", func(t *testing.T) {
		_, err := conn.Exec(ctx, "DROP TABLE IF EXISTS e2e_schema.e2e_users")
		if err != nil {
			t.Fatalf("DROP TABLE failed: %v", err)
		}
		t.Log("DROP TABLE e2e_schema.e2e_users OK")

		// 验证表已不存在
		_, err = conn.Exec(ctx, "SELECT * FROM e2e_schema.e2e_users")
		if err == nil {
			t.Error("expected SELECT to fail on dropped table")
		} else {
			t.Logf("Verified: SELECT on dropped table fails: %v", err)
		}

		// 清理 schema
		_, err = conn.Exec(ctx, "DROP SCHEMA IF EXISTS e2e_schema CASCADE")
		if err != nil {
			t.Fatalf("DROP SCHEMA failed: %v", err)
		}
		t.Log("DROP SCHEMA e2e_schema OK")
	})

	t.Log("PgSQL comprehensive E2E PASSED")
}
