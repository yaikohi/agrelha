package server

import (
	"github.com/gofiber/fiber/v2"
)

// valheimConsole renders the Valheim console/logs page.
func (s *FiberServer) valheimConsole(c *fiber.Ctx) error {
	return s.ensureConsoleHandler().ValheimConsole(c)
}
