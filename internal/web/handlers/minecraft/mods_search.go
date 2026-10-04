package minecraft

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/domain"
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
		return patchModResults(c, `<p class="col-span-full py-6 text-center text-xs text-red-400">Mod search is unconfigured.</p>`)
	}

	var req struct {
		Q        string `json:"q" form:"q"`
		ModQuery string `json:"modQuery" form:"modQuery"`
	}
	_ = c.BodyParser(&req)

	q := firstNonEmpty(req.ModQuery, req.Q, c.Query("q"))
	if q == "" {
		return patchModResults(c, `<p class="col-span-full py-6 text-center text-xs text-zinc-500">Search Modrinth to find mods by name.</p>`)
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
		return patchModResults(c, fmt.Sprintf(`<p class="col-span-full py-6 text-center text-xs text-red-400">Search failed: %s</p>`, html.EscapeString(err.Error())))
	}
	if len(hits) == 0 {
		return patchModResults(c, fmt.Sprintf(`<p class="col-span-full py-6 text-center text-xs text-zinc-500">No mods found for %q on Minecraft %s.</p>`, html.EscapeString(q), html.EscapeString(mcVersion)))
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		`<div class="col-span-full pb-1 text-xs font-medium text-zinc-400">Results for "%s" (%d) — compatible with %s</div>`,
		html.EscapeString(q), len(hits), html.EscapeString(mcVersion)))

	for _, hit := range hits {
		provider := hit.Provider
		if provider == "" {
			provider = domain.ProviderModrinth
		}
		ref, _ := domain.ParseMCModRef(hit.Slug, provider)
		sb.WriteString(modCardHTML(num, hit, installed[ref.Key()]))
	}
	return patchModResults(c, sb.String())
}

func modCardHTML(num int, hit ModHit, installed bool) string {
	slug := strings.ReplaceAll(hit.Slug, "'", "\\'")
	provider := hit.Provider
	if provider == "" {
		provider = domain.ProviderModrinth
	}

	action := fmt.Sprintf(
		`<button type="button" data-on:click="@post('/api/minecraft/%d/mods/install', {payload: {slug: '%s', provider: '%s'}})" class="shrink-0 rounded-lg bg-zinc-100 px-3 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white transition">+ Install</button>`,
		num, slug, provider)

	// A Restricted mod exists and can be read about, but no download exists for
	// anyone but CurseForge's own client. Offering Install would be a lie.
	if hit.Restricted {
		action = `<span class="shrink-0 rounded-lg border border-amber-800/60 bg-amber-950/40 px-3 py-1.5 text-xs font-medium text-amber-300" title="This mod's author does not allow third-party downloads. No API key changes this.">Not downloadable</span>`
	}

	badge := providerBadge(provider)
	if installed {
		badge += ` <span class="rounded bg-emerald-950/70 border border-emerald-800/60 px-1.5 py-0.5 text-[10px] font-medium text-emerald-300">Installed</span>`
		action = fmt.Sprintf(
			`<button type="button" data-on:click="@post('/api/minecraft/%d/mods/remove', {payload: {slug: '%s', provider: '%s'}})" class="shrink-0 rounded-lg bg-red-950/50 border border-red-800/60 px-3 py-1.5 text-xs font-medium text-red-300 hover:bg-red-900/60 transition">Remove</button>`,
			num, slug, provider)
	}

	return fmt.Sprintf(`
		<div class="flex items-center justify-between gap-3 rounded-xl border border-zinc-800 bg-zinc-950 p-3 hover:border-zinc-700 transition">
			<div class="flex min-w-0 items-center gap-3">
				<img src="%s" alt="" class="h-9 w-9 shrink-0 rounded-lg bg-zinc-800 object-cover" onerror="this.style.display='none'"/>
				<div class="min-w-0">
					<div class="flex flex-wrap items-center gap-1.5">
						<span class="text-sm font-medium text-zinc-100">%s</span>
						<span class="font-mono text-[10px] text-zinc-500">%s</span>
						%s
					</div>
					<p class="mt-0.5 line-clamp-2 text-[11px] text-zinc-400">%s</p>
				</div>
			</div>
			%s
		</div>`,
		html.EscapeString(hit.IconURL), html.EscapeString(hit.Title),
		html.EscapeString(hit.Slug), badge, html.EscapeString(hit.Description), action)
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

// providerBadge names which catalogue a hit came from. With one search box over
// two catalogues, the operator otherwise cannot tell why the same-looking mod
// appears twice.
func providerBadge(p domain.Provider) string {
	if p == domain.ProviderCurseForge {
		return `<span class="rounded bg-orange-950/60 border border-orange-800/60 px-1.5 py-0.5 text-[10px] font-medium text-orange-300">CurseForge</span>`
	}
	return `<span class="rounded bg-green-950/60 border border-green-800/60 px-1.5 py-0.5 text-[10px] font-medium text-green-300">Modrinth</span>`
}
