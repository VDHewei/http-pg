package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/VDHewei/http-pg/internal/config"
	"github.com/VDHewei/http-pg/pkg/handler"
	"github.com/VDHewei/http-pg/pkg/mysqlpool"
	"github.com/VDHewei/http-pg/pkg/pgpool"
)

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	flag.Parse()

	// 加载配置
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if cfg.EncryptionKey == "" {
		log.Fatal("Encryption key is required. Set ENCRYPTION_KEY env var or encryption_key in config.json")
	}

	// 初始化 PgSQL 连接池
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pgPool, err := pgpool.New(ctx, cfg.PostgresDSN, cfg.MaxConnections, cfg.MinConnections)
	if err != nil {
		log.Fatalf("Failed to create PgSQL pool: %v", err)
	}
	defer pgPool.Close()

	log.Printf("Connected to PostgreSQL: %s", cfg.PostgresDSN)

	// 根据配置初始化 MySQL 连接池（仅在 proxy_protocol 为 mysql 或 both 时）
	var mysqlPool handler.Pool
	if cfg.ProxyProtocol == "mysql" || cfg.ProxyProtocol == "both" {
		myPool, err := mysqlpool.New(ctx, cfg.MySQLDSN, cfg.MaxConnections, cfg.MinConnections)
		if err != nil {
			log.Printf("WARNING: Failed to create MySQL pool: %v", err)
		} else {
			mysqlPool = myPool
			defer mysqlPool.Close()
			log.Printf("Connected to MySQL: %s", cfg.MySQLDSN)
		}
	}

	// 创建框架无关的核心处理器
	coreHandler, err := handler.NewHandler(
		handler.NewPgPoolAdapter(pgPool),
		mysqlPool, // MySQL 池（阶段五启用）
		cfg.EncryptionKey,
	)
	if err != nil {
		log.Fatalf("Failed to create handler: %v", err)
	}

	// 通过 build tags 选择 HTTP 框架并启动服务
	serveHTTP(cfg, coreHandler)
}

// waitForShutdown 监听系统信号并优雅关闭
func waitForShutdown() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("Shutting down server...")
}
