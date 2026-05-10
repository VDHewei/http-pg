package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/VDHewei/http-pg/internal/config"
	"github.com/VDHewei/http-pg/pkg/mysqlproxy"
	"github.com/VDHewei/http-pg/pkg/pgproxy"
)

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	flag.Parse()

	// 加载并验证配置
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid config: %v", err)
	}

	// 获取服务端 URL（支持配置化，向后兼容）
	serverURL := cfg.GetServerURL()

	log.Printf("Proxy starting (protocol: %s)", cfg.ProxyProtocol)
	log.Printf("HTTP API server at %s", serverURL)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 根据配置启动 PgSQL 代理
	if cfg.ProxyProtocol == "pg" || cfg.ProxyProtocol == "both" {
		pgProxy, err := pgproxy.New(cfg.ProxyAddr, serverURL, cfg.EncryptionKey)
		if err != nil {
			log.Fatalf("Failed to create PgSQL proxy: %v", err)
		}

		go func() {
			log.Printf("[PgSQL Proxy] Starting on %s", cfg.ProxyAddr)
			if err := pgProxy.Start(); err != nil {
				log.Fatalf("[PgSQL Proxy] Error: %v", err)
			}
		}()

		// 退出时关闭
		defer pgProxy.Stop()
	}

	// 根据配置启动 MySQL 代理
	if cfg.ProxyProtocol == "mysql" || cfg.ProxyProtocol == "both" {
		myProxy, err := mysqlproxy.New(cfg.MySQLProxyAddr, serverURL, cfg.EncryptionKey)
		if err != nil {
			log.Fatalf("Failed to create MySQL proxy: %v", err)
		}

		go func() {
			log.Printf("[MySQL Proxy] Starting on %s", cfg.MySQLProxyAddr)
			if err := myProxy.Start(); err != nil {
				log.Fatalf("[MySQL Proxy] Error: %v", err)
			}
		}()

		// 退出时关闭
		defer myProxy.Stop()
	}

	<-sigCh
	log.Println("Shutting down...")
	log.Println("Proxy stopped")
}
