package server

import (
	"github.com/gofiber/fiber/v2"
)

func (s *FiberServer) historyPage(c *fiber.Ctx) error {
	return s.ensureAccessHandler().HistoryPage(c)
}
