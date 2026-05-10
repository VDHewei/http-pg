package adapter

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/VDHewei/http-pg/pkg/handler"
)

// RegisterGinRoutes 将核心 Handler 的业务方法注册到 Gin 路由
// 每个路由对应一个 Gin 处理函数，负责 HTTP 层的请求解析和响应写入
func RegisterGinRoutes(router gin.IRouter, h *handler.Handler) {
	router.GET("/health", ginHealthCheck(h))
	router.POST("/session", ginCreateSession(h))
	router.POST("/query", ginHandleQuery(h))
	router.DELETE("/session/:session_id", ginCloseSession(h))
}

// ginCreateSession 创建数据库会话的 Gin 处理器
// POST /api/v1/session
//   - Header: X-Protocol（可选，"pg" 或 "mysql"，默认 "pg"）
//   - Body: 加密的连接参数（application/octet-stream）
//   - Response: 纯文本 session UUID
func ginCreateSession(h *handler.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "read body: " + err.Error()})
			return
		}

		protocol := c.GetHeader("X-Protocol")
		if protocol == "" {
			protocol = string(handler.ProtocolPgSQL)
		}

		sessionID, err := h.CreateSession(c.Request.Context(), body, handler.ProtocolType(protocol))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.String(http.StatusOK, sessionID)
	}
}

// ginHandleQuery 处理 SQL 查询的 Gin 处理器
// POST /api/v1/query
//   - Header: X-Session-ID（会话标识符）
//   - Body: 加密的 QueryRequest JSON（application/octet-stream）
//   - Response: 加密的 QueryResponse JSON（application/octet-stream）
func ginHandleQuery(h *handler.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("X-Session-ID")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing X-Session-ID header"})
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "read body: " + err.Error()})
			return
		}

		encrypted, err := h.HandleQuery(c.Request.Context(), sessionID, body)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.Data(http.StatusOK, "application/octet-stream", encrypted)
	}
}

// ginCloseSession 关闭数据库会话的 Gin 处理器
// DELETE /api/v1/session/:session_id
func ginCloseSession(h *handler.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("session_id")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing session_id"})
			return
		}

		h.CloseSession(c.Request.Context(), sessionID)
		c.JSON(http.StatusOK, gin.H{"status": "closed"})
	}
}

// ginHealthCheck 健康检查的 Gin 处理器
// GET /api/v1/health
func ginHealthCheck(h *handler.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		status := h.HealthCheck()
		c.JSON(http.StatusOK, status)
	}
}
