package e2e

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/VDHewei/http-pg/pkg/handler"
	"github.com/VDHewei/http-pg/pkg/handler/adapter"
	"github.com/VDHewei/http-pg/pkg/mysqlpool"
	"github.com/VDHewei/http-pg/pkg/mysqlproxy"
	"github.com/VDHewei/http-pg/pkg/pgpool"

	_ "github.com/go-sql-driver/mysql"
)

// freePort 获取一个可用的 TCP 端口
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("get free port: %v", err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}

const (
	postgresDSN  = "postgres://postgres:postgres@127.0.0.1:5432/testdb?sslmode=disable"
	mysqlDSN     = "root:root@tcp(127.0.0.1:3306)/testdb"
	encKey       = "http-pg-e2e-test-key-32chars!!"
)

// startServer 启动 HTTP 服务器，返回 serverURL 和 close 函数
func startServer(t *testing.T, pgPool, myPool handler.Pool) (string, func()) {
	t.Helper()

	h, err := handler.NewHandler(pgPool, myPool, encKey)
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	adapter.RegisterGinRoutes(r.Group("/api/v1"), h)

	port := freePort(t)
	addr := "127.0.0.1:" + port
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listen HTTP: %v", err)
	}
	go func() { _ = r.RunListener(l) }()
	time.Sleep(300 * time.Millisecond)

	return "http://" + addr, func() { l.Close() }
}

// startMySQLProxy 启动 MySQL 代理
func startMySQLProxy(t *testing.T, serverURL string) (*mysqlproxy.Proxy, string) {
	t.Helper()
	port := freePort(t)
	p, err := mysqlproxy.New("0.0.0.0:"+port, serverURL, encKey)
	if err != nil {
		t.Fatalf("create mysql proxy: %v", err)
	}
	go func() { _ = p.Start() }()
	time.Sleep(200 * time.Millisecond)
	return p, "127.0.0.1:" + port
}

// TestMySQLComprehensive 使用真实 MySQL 代理执行综合 SQL 测试
//
// 链路: go-sql-driver/mysql → TCP → MySQL Proxy → HTTP → Handler → mysqlpool → Docker MySQL
func TestMySQLComprehensive(t *testing.T) {
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

	// 2. 启动 HTTP 服务器 + MySQL 代理
	serverURL, stopServer := startServer(t, handler.NewPgPoolAdapter(pgPool), myPool)
	defer stopServer()

	proxy, proxyAddr := startMySQLProxy(t, serverURL)
	defer proxy.Stop()

	// 3. MySQL 客户端连接代理
	dsn := fmt.Sprintf("root:root@tcp(%s)/testdb?timeout=5s&readTimeout=5s&writeTimeout=5s", proxyAddr)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	defer db.Close()

	// ========== DDL: 创库 / 创表 ==========

	// 注: MySQL 8.0 连接时已指定 database=testdb，无需单独 CREATE DATABASE

	t.Run("CREATE_TABLE", func(t *testing.T) {
		_, err := db.Exec("DROP TABLE IF EXISTS e2e_users")
		if err != nil {
			t.Fatalf("DROP TABLE failed: %v", err)
		}
		_, err = db.Exec(`
			CREATE TABLE e2e_users (
				id INT AUTO_INCREMENT PRIMARY KEY,
				name VARCHAR(100) NOT NULL,
				email VARCHAR(200),
				age INT DEFAULT 0
			)
		`)
		if err != nil {
			t.Fatalf("CREATE TABLE failed: %v", err)
		}
		t.Log("CREATE TABLE e2e_users OK")
	})

	// ========== DML: 插入 ==========

	t.Run("INSERT", func(t *testing.T) {
		result, err := db.Exec("INSERT INTO e2e_users (name, email, age) VALUES ('Alice', 'alice@test.com', 30)")
		if err != nil {
			t.Fatalf("INSERT Alice failed: %v", err)
		}
		n, _ := result.RowsAffected()
		t.Logf("INSERT Alice: affected=%d", n)

		_, err = db.Exec("INSERT INTO e2e_users (name, email, age) VALUES ('Bob', 'bob@test.com', 25)")
		if err != nil {
			t.Fatalf("INSERT Bob failed: %v", err)
		}
		t.Log("INSERT Bob OK")
	})

	// ========== DML: 查询 ==========

	t.Run("SELECT", func(t *testing.T) {
		rows, err := db.Query("SELECT id, name, email, age FROM e2e_users ORDER BY id")
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
		result, err := db.Exec("UPDATE e2e_users SET name='Alice Updated', age=31 WHERE name='Alice'")
		if err != nil {
			t.Fatalf("UPDATE failed: %v", err)
		}
		n, _ := result.RowsAffected()
		t.Logf("UPDATE affected=%d", n)

		var name string
		err = db.QueryRow("SELECT name FROM e2e_users WHERE id=1").Scan(&name)
		if err != nil {
			t.Fatalf("SELECT after UPDATE failed: %v", err)
		}
		if name != "Alice Updated" {
			t.Errorf("expected 'Alice Updated', got %q", name)
		}
		t.Logf("Verified: name=%s", name)
	})

	// ========== DML: 删除记录 ==========

	t.Run("DELETE", func(t *testing.T) {
		_, err := db.Exec("INSERT INTO e2e_users (name, email, age) VALUES ('Tmp', 'tmp@test.com', 1)")
		if err != nil {
			t.Fatalf("INSERT tmp failed: %v", err)
		}

		result, err := db.Exec("DELETE FROM e2e_users WHERE name='Tmp'")
		if err != nil {
			t.Fatalf("DELETE failed: %v", err)
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			t.Errorf("DELETE expected 1 row affected, got %d", n)
		}
		t.Logf("DELETE affected=%d", n)
	})

	// ========== DDL: 改表 (ALTER TABLE) ==========

	t.Run("ALTER_TABLE", func(t *testing.T) {
		_, err := db.Exec("ALTER TABLE e2e_users ADD COLUMN phone VARCHAR(20)")
		if err != nil {
			t.Fatalf("ALTER TABLE ADD COLUMN failed: %v", err)
		}
		t.Log("ALTER TABLE ADD COLUMN phone OK")

		// 验证新列
		_, err = db.Exec("UPDATE e2e_users SET phone='12345' WHERE id=1")
		if err != nil {
			t.Fatalf("UPDATE phone failed: %v", err)
		}
		var phone string
		err = db.QueryRow("SELECT phone FROM e2e_users WHERE id=1").Scan(&phone)
		if err != nil {
			t.Fatalf("SELECT phone failed: %v", err)
		}
		if phone != "12345" {
			t.Errorf("expected phone='12345', got %q", phone)
		}
		t.Logf("Verified: phone=%s", phone)
	})

	// ========== DCL: 创建用户 / 授权 / 移除授权 ==========

	t.Run("CREATE_USER_GRANT_REVOKE", func(t *testing.T) {
		// 清理旧用户
		db.Exec("DROP USER IF EXISTS 'e2e_testuser'@'%'")

		// 创建用户
		_, err := db.Exec("CREATE USER 'e2e_testuser'@'%' IDENTIFIED BY 'testpass'")
		if err != nil {
			t.Fatalf("CREATE USER failed: %v", err)
		}
		t.Log("CREATE USER e2e_testuser OK")

		// 授权
		_, err = db.Exec("GRANT SELECT ON testdb.* TO 'e2e_testuser'@'%'")
		if err != nil {
			t.Fatalf("GRANT SELECT failed: %v", err)
		}
		t.Log("GRANT SELECT OK")

		// 验证授权: 以新用户身份连接
		testUserDSN := fmt.Sprintf("e2e_testuser:testpass@tcp(%s)/testdb?timeout=5s&readTimeout=5s", proxyAddr)
		testDB, err := sql.Open("mysql", testUserDSN)
		if err != nil {
			t.Fatalf("open testuser connection: %v", err)
		}
		defer testDB.Close()

		var count int
		err = testDB.QueryRow("SELECT COUNT(*) FROM e2e_users").Scan(&count)
		if err != nil {
			t.Fatalf("SELECT as testuser failed: %v", err)
		}
		t.Logf("Verified: testuser can connect, count=%d", count)

		// 移除授权
		_, err = db.Exec("REVOKE SELECT ON testdb.* FROM 'e2e_testuser'@'%'")
		if err != nil {
			t.Fatalf("REVOKE SELECT failed: %v", err)
		}
		t.Log("REVOKE SELECT OK")
	})

	// ========== 切换用户账号 ==========

	t.Run("USER_SWITCHING", func(t *testing.T) {
		// 创建两个用户
		db.Exec("DROP USER IF EXISTS 'e2e_user_a'@'%'")
		db.Exec("DROP USER IF EXISTS 'e2e_user_b'@'%'")
		db.Exec("CREATE USER 'e2e_user_a'@'%' IDENTIFIED BY 'pass_a'")
		db.Exec("CREATE USER 'e2e_user_b'@'%' IDENTIFIED BY 'pass_b'")
		db.Exec("GRANT SELECT ON testdb.e2e_users TO 'e2e_user_a'@'%'")
		db.Exec("GRANT SELECT ON testdb.e2e_users TO 'e2e_user_b'@'%'")

		// 用户 A 连接
		dsnA := fmt.Sprintf("e2e_user_a:pass_a@tcp(%s)/testdb?timeout=5s", proxyAddr)
		dbA, err := sql.Open("mysql", dsnA)
		if err != nil {
			t.Fatalf("User A connection failed: %v", err)
		}
		defer dbA.Close()

		var result string
		if err := dbA.QueryRow("SELECT name FROM e2e_users WHERE id=1").Scan(&result); err != nil {
			t.Fatalf("User A QueryRow Scan failed: %v", err)
		}
		t.Logf("User A read: name=%s", result)

		// 用户 B 连接 (模拟切换)
		dsnB := fmt.Sprintf("e2e_user_b:pass_b@tcp(%s)/testdb?timeout=5s", proxyAddr)
		dbB, err := sql.Open("mysql", dsnB)
		if err != nil {
			t.Fatalf("User B connection failed: %v", err)
		}
		defer dbB.Close()

		if err := dbB.QueryRow("SELECT name FROM e2e_users WHERE id=2").Scan(&result); err != nil {
			t.Fatalf("User B QueryRow Scan failed: %v", err)
		}
		t.Logf("User B read: name=%s", result)

		t.Log("User switching verified (both users can access their data)")

		// 清理
		db.Exec("DROP USER IF EXISTS 'e2e_user_a'@'%'")
		db.Exec("DROP USER IF EXISTS 'e2e_user_b'@'%'")
	})

	// ========== DDL: 删表 ==========

	t.Run("DROP_TABLE", func(t *testing.T) {
		_, err := db.Exec("DROP TABLE IF EXISTS e2e_users")
		if err != nil {
			t.Fatalf("DROP TABLE failed: %v", err)
		}
		t.Log("DROP TABLE e2e_users OK")

		// 验证表已不存在
		_, err = db.Exec("SELECT * FROM e2e_users")
		if err == nil {
			t.Error("expected SELECT to fail on dropped table")
		} else {
			t.Logf("Verified: SELECT on dropped table fails: %v", err)
		}
	})

	t.Log("MySQL comprehensive E2E PASSED")
}
