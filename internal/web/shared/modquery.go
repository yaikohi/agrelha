package shared

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/domain"
)

// ParseModQuery reads one catalogue search from the request, whichever way the
// caller phrased it. Every surface has its own signal name for the text box and
// the wizard pages also search by query string, so the aliases are kept rather
// than renamed in five templates at once.
func ParseModQuery(c *fiber.Ctx) domain.ModQuery {
	var req struct {
		Q               string `json:"q" form:"q"`
		ModQuery        string `json:"modQuery" form:"modQuery"`
		ModSearch       string `json:"modSearch" form:"modSearch"`
		WizardModSearch string `json:"wizardModSearch" form:"wizardModSearch"`
		Sort            string `json:"sort" form:"sort"`
		Deprecated      bool   `json:"deprecated" form:"deprecated"`
		Limit           int    `json:"limit" form:"limit"`
	}
	_ = c.BodyParser(&req)

	q := domain.ModQuery{
		Text:       firstText(req.WizardModSearch, req.ModSearch, req.ModQuery, req.Q, c.Query("q")),
		Sort:       domain.ParseModSort(firstText(req.Sort, c.Query("sort"))),
		Deprecated: req.Deprecated || c.Query("deprecated") == "true",
		Limit:      req.Limit,
	}
	if q.Limit <= 0 {
		if l, err := strconv.Atoi(c.Query("limit")); err == nil {
			q.Limit = l
		}
	}
	return q.WithDefaults()
}

func firstText(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
