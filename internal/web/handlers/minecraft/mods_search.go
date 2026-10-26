package minecraft

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/gofiber/fiber/v2"

	"agrelha/internal/domain"
	"agrelha/internal/web/pages"
)

const modResultsSelector = "#mc-mod-results"

// ModHit is one Modrinth search result, as the instance mods page needs it.
type ModHit struct {
	Slug        string
	Title       string
	Description string
	IconURL     string
	// Provider says which catalogue the hit came from, and therefore which file
	// the Install writes to. Empty means Modrinth, so existing callers are
	// unaffected.
	Provider domain.Provider
	// Restricted marks a mod whose author forbids third-party distribution. It
	// can be read about but never added, so Install is offered but refused.
	Restricted bool
}

// SearchModsFunc searches the content provider for mods matching a query.
type SearchModsFunc func(ctx context.Context, query, mcVersion, loader string) ([]ModHit, error)

// MCInstanceModsSearch renders Modrinth results into the instance mods page,
// so mods can be found by name rather than typed as an exact slug.
func (h *Handler) MCInstanceModsSearch(c *fiber.Ctx) error {
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}
	if h.cfg.SearchMods == nil {
		return patchModResults(c, `<p class="px-6 py-8 font-pixel text-[11px] text-danger">Mod search is unconfigured.</p>`)
	}

	var req struct {
		Q        string `json:"q" form:"q"`
		ModQuery string `json:"modQuery" form:"modQuery"`
	}
	_ = c.BodyParser(&req)

	q := firstNonEmpty(req.ModQuery, req.Q, c.Query("q"))
	if q == "" {
		return patchModResults(c, `<p class="px-6 py-10 font-pixel text-[11px] text-ink-faint">Search Modrinth to find mods by name.</p>`)
	}

	mcVersion := ""
	loader := ""
	installed := map[string]bool{}
	if h.cfg.MCInstances != nil {
		if inst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), num); err == nil && inst != nil {
			if inst.Minecraft != nil {
				mcVersion = inst.Minecraft.MCVersion
				loader = string(inst.Minecraft.Loader)
			}
		}
		// Keyed by Provider as well as slug: the same slug on Modrinth and on
		// CurseForge is two different mods, and marking one Installed because the
		// other is would offer the wrong button.
		if mods, err := h.cfg.MCInstances.GetInstalledMods(c.UserContext(), num); err == nil {
			for _, m := range mods {
				if ref, ok := domain.ParseMCModRef(m, domain.ProviderModrinth); ok {
					installed[ref.Key()] = true
				}
			}
		}
		if mods, err := h.cfg.MCInstances.GetCurseForgeMods(c.UserContext(), num); err == nil {
			for _, m := range mods {
				if ref, ok := domain.ParseMCModRef(m, domain.ProviderCurseForge); ok {
					installed[ref.Key()] = true
				}
			}
		}
	}

	hits, err := h.cfg.SearchMods(c.UserContext(), q, mcVersion, loader)
	if err != nil {
		return patchModResults(c, fmt.Sprintf(`<p class="px-6 py-8 font-pixel text-[11px] text-danger">Search failed: %s</p>`, html.EscapeString(err.Error())))
	}
	if len(hits) == 0 {
		return patchModResults(c, fmt.Sprintf(`<p class="px-6 py-10 font-pixel text-[11px] text-ink-faint">No mods found for %q on Minecraft %s.</p>`, html.EscapeString(q), html.EscapeString(mcVersion)))
	}

	ui := pages.ModResultsUI{
		Game:    "minecraft",
		Num:     num,
		Query:   q,
		Heading: fmt.Sprintf("%d results for %q", len(hits), q),
		Note:    "compatible with " + mcVersion,
	}
	for _, hit := range hits {
		provider := hit.Provider
		if provider == "" {
			provider = domain.ProviderModrinth
		}
		ref, _ := domain.ParseMCModRef(hit.Slug, provider)
		ui.Results = append(ui.Results, pages.ModResultUI{
			Title:       hit.Title,
			Slug:        hit.Slug,
			Version:     "",
			IconURL:     hit.IconURL,
			Description: hit.Description,
			Provider:    string(provider),
			Installed:   installed[ref.Key()],
			Restricted:  hit.Restricted,
		})
	}
	return patchModResults(c, renderComponent(pages.ModSearchResults(ui)))
}

func renderComponent(comp templ.Component) string {
	var sb strings.Builder
	if err := comp.Render(context.Background(), &sb); err != nil {
		return ""
	}
	return sb.String()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func patchModResults(c *fiber.Ctx, content string) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := innerElement(w, modResultsSelector, content); err != nil {
		return err
	}
	return c.Send(buf.Bytes())
}
