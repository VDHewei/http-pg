//go:build fiber

package adapter

import (
	"github.com/gofiber/fiber/v3"

	"github.com/http-pg/http-pg/pkg/handler"
)

// RegisterFiberRoutes 将核心 Handler 的业务方法注册到 Fiber 路由
// 仅在编译时指定 -tags fiber 时生效
func RegisterFiberRoutes(app *fiber.App, h *handler.Handler) {
	api := app.Group("/api/v1")

	api.Get("/health", fiberHealthCheck(h))
	api.Post("/session", fiberCreateSession(h))
	api.Post("/query", fiberHandleQuery(h))
	api.Delete("/session/:session_id", fiberCloseSession(h))
}

// fiberCreateSession 创建数据库会话的 Fiber 处理器
// POST /api/v1/session
//   - Body: 加密的连接参数（application/octet-stream）
//   - Response: 纯文本 session UUID
func fiberCreateSession(h *handler.Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		body := c.Body()

		sessionID, err := h.CreateSession(c.Context(), body, handler.ProtocolPgSQL)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		return c.Status(fiber.StatusOK).SendString(sessionID)
	}
}

// fiberHandleQuery 处理 SQL 查询的 Fiber 处理器
// POST /api/v1/query
//   - Header: X-Session-ID（会话标识符）
//   - Body: 加密的 QueryRequest JSON（application/octet-stream）
//   - Response: 加密的 QueryResponse JSON（application/octet-stream）
func fiberHandleQuery(h *handler.Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		sessionID := c.Get("X-Session-ID")
		if sessionID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "missing X-Session-ID header",
			})
		}

		body := c.Body()

		encrypted, err := h.HandleQuery(c.Context(), sessionID, body)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		c.Set("Content-Type", "application/octet-stream")
		return c.Status(fiber.StatusOK).Send(encrypted)
	}
}

// fiberCloseSession 关闭数据库会话的 Fiber 处理器
// DELETE /api/v1/session/:session_id
func fiberCloseSession(h *handler.Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		sessionID := c.Params("session_id")
		if sessionID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "missing session_id",
			})
		}

		h.CloseSession(c.Context(), sessionID)
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "closed"})
	}
}

// fiberHealthCheck 健康检查的 Fiber 处理器
// GET /api/v1/health
func fiberHealthCheck(h *handler.Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		status := h.HealthCheck()
		return c.Status(fiber.StatusOK).JSON(status)
	}
}
