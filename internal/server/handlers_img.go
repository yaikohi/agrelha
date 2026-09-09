package server

import (
	"github.com/gofiber/fiber/v2"
)

func (s *FiberServer) imageProxy(c *fiber.Ctx) error {
	return s.ensureContentHandler().ImageProxy(c)
}
