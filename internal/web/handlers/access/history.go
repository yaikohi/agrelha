package access

import (
	"agrelha/internal/infra/store"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"

	"github.com/gofiber/fiber/v2"
)

func (h *Handler) HistoryPage(c *fiber.Ctx) error {
	var entries []store.HistoryEntry
	if h.cfg.Store != nil {
		entries, _ = h.cfg.Store.ListHistory(200)
	}
	return shared.Render(c, pages.History(entries))
}
