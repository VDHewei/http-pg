//go:build !fiber

package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/VDHewei/http-pg/internal/config"
	"github.com/VDHewei/http-pg/pkg/handler"
	"github.com/VDHewei/http-pg/pkg/handler/adapter"
)

// serveHTTP 使用 Gin 框架启动 HTTP 服务
// 仅在未指定 -tags fiber 时编译
func serveHTTP(cfg *config.Config, h *handler.Handler) {
	// 设置 Gin 为发布模式
	gin.SetMode(gin.ReleaseMode)

	// 创建 Gin 路由并注册中间件
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	// 注册核心业务路由到 Gin
	api := r.Group("/api/v1")
	adapter.RegisterGinRoutes(api, h)

	// 创建 HTTP 服务器
	srv := &http.Server{
		Addr:    cfg.ServerAddr,
		Handler: r.Handler(),
	}

	// 启动服务器
	go func() {
		log.Printf("HTTP API Server (Gin) starting on %s", cfg.ServerAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// 等待关闭信号
	waitForShutdown()

	// 优雅关闭
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped gracefully")
}
