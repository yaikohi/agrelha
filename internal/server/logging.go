package server

import (
	"agrelha/internal/web/shared"

	"github.com/gofiber/fiber/v2"
)

func requestLogger() fiber.Handler {
	return shared.RequestLogger()
}

func rid(c *fiber.Ctx) string {
	return shared.RequestID(c)
}
