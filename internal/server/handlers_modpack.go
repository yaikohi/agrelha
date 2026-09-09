package server

import (
	"context"

	"github.com/gofiber/fiber/v2"
)

// withBepInEx delegates to contentHandler.WithBepInEx.
func (s *FiberServer) withBepInEx(ctx context.Context, entries []string) []string {
	return s.ensureContentHandler().WithBepInEx(ctx, entries)
}

func (s *FiberServer) modpackExport(c *fiber.Ctx) error {
	return s.ensureContentHandler().ModpackExport(c)
}
