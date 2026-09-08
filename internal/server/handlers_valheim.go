package server

import (
	"github.com/gofiber/fiber/v2"

	"agrelha/cmd/web/pages"
)

// valheimConsole renders the Valheim console/logs page. It is the Valheim
// counterpart to a Minecraft instance's "Console & Logs" tab.
func (s *FiberServer) valheimConsole(c *fiber.Ctx) error {
	fk, fm := takeFlash(c)
	return render(c, pages.ValheimConsole(s.isAdmin(c), fk, fm))
}
