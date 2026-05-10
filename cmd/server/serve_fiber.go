//go:build fiber

package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v3"

	"github.com/VDHewei/http-pg/internal/config"
	"github.com/VDHewei/http-pg/pkg/handler"
	"github.com/VDHewei/http-pg/pkg/handler/adapter"
)

// serveHTTP 使用 Fiber 框架启动 HTTP 服务
// 仅在指定 -tags fiber 时编译
func serveHTTP(cfg *config.Config, h *handler.Handler) {
	// 创建 Fiber 应用
	app := fiber.New(fiber.Config{
		ServerHeader:   "HTTP-PG",
		ReadBufferSize: 8192,
	})

	// 注册核心业务路由到 Fiber
	adapter.RegisterFiberRoutes(app, h)

	// 启动服务器
	go func() {
		log.Printf("HTTP API Server (Fiber) starting on %s", cfg.ServerAddr)
		if err := app.Listen(cfg.ServerAddr); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// 等待关闭信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("Shutting down server...")

	// 优雅关闭 Fiber
	if err := app.Shutdown(); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped gracefully")
}
