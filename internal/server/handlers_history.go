package server

import (
	"github.com/gofiber/fiber/v2"

	"agrelha/cmd/web/pages"
)

func (s *FiberServer) historyPage(c *fiber.Ctx) error {
	entries, _ := s.store.ListHistory(200)
	return render(c, pages.History(entries))
}
