package server

import (
	"agrelha/internal/web/shared"

	"github.com/gofiber/fiber/v2"
)

func setFlash(c *fiber.Ctx, kind, msg string) {
	shared.SetFlash(c, kind, msg)
}

func takeFlash(c *fiber.Ctx) (kind, msg string) {
	return shared.TakeFlash(c)
}
