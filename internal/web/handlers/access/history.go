package access

import (
	"agrelha/internal/domain"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"

	"github.com/gofiber/fiber/v2"
)

func (h *Handler) HistoryPage(c *fiber.Ctx) error {
	var entries []domain.HistoryEntry
	if h.cfg.History != nil {
		entries, _ = h.cfg.History.ListHistory(200)
	}
	return shared.Render(c, pages.History(entries))
}
