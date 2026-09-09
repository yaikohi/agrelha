package server

import (
	"github.com/gofiber/fiber/v2"

	"agrelha/internal/web/shared"
)

func sseToast(c *fiber.Ctx, kind, msg string, extra map[string]any) error {
	return shared.SSEToast(c, kind, msg, extra)
}

func (s *FiberServer) modsUpdateAll(c *fiber.Ctx) error {
	return s.ensureContentHandler().ModsUpdateAll(c)
}

func (s *FiberServer) modsUpdateSelected(c *fiber.Ctx) error {
	return s.ensureContentHandler().ModsUpdateSelected(c)
}

func (s *FiberServer) applyUpdates(c *fiber.Ctx, targetKeys []string) error {
	return s.ensureContentHandler().ApplyUpdates(c, targetKeys)
}
