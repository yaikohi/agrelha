package valheim

import (
	"bufio"
	"bytes"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
	"agrelha/internal/web/sse"
)

// ValheimInstancePage renders the Valheim instance detail tab view.
func (h *Handler) ValheimInstancePage(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Valheim instance manager not configured")
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}

	tab := strings.ToLower(strings.TrimSpace(c.Params("tab", "overview")))
	if tab == "" {
		tab = "overview"
	}

	inst, err := h.cfg.ValheimInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Valheim instance not found")
	}

	d := pages.InstanceDetailUI{
		InstanceUI: pages.InstanceUI{
			GameID:    string(inst.GameID),
			Number:    inst.Number,
			Name:      inst.Name,
			Slug:      inst.Slug,
			Password:  inst.Password,
			Seed:      inst.Seed,
			Tier:      string(inst.Tier),
			MemoryGiB: inst.MemoryGiB(),
			State:     string(inst.State),
			MOTD:      inst.MOTD,
			LBIP:      inst.LBIP,
		},
		ActiveTab: tab,
	}

	// Fetch installed mods if on mods tab or overview
	if tab == "mods" || tab == "overview" {
		if mods, err := h.cfg.ValheimInstances.GetInstalledMods(c.UserContext(), inst.Number); err == nil {
			d.InstalledMods = mods
		}
	}

	// Fetch config files if on configs tab or overview
	if tab == "configs" || tab == "overview" {
		if cfgs, err := h.cfg.ValheimInstances.ListConfigs(c.UserContext(), inst.Number); err == nil {
			d.ConfigFiles = cfgs
		}
	}

	// Fetch backups if on backups tab
	if tab == "backups" {
		for _, b := range h.cfg.ValheimInstances.ListBackups(*inst) {
			d.Backups = append(d.Backups, pages.BackupUI{
				Name:      b.Name,
				SizeBytes: b.SizeBytes,
				CreatedAt: b.CreatedAt,
			})
		}
	}

	return shared.Render(c, pages.ValheimInstanceDetail(d))
}

// ValheimInstanceSettingsSave saves settings (name, password, tier) for a Valheim instance.
func (h *Handler) ValheimInstanceSettingsSave(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	var req struct {
		Name     string `json:"name" form:"name"`
		Password string `json:"password" form:"password"`
		Tier     string `json:"tier" form:"tier"`
	}
	_ = c.BodyParser(&req)

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.TrimSpace(c.FormValue("name"))
	}
	pwd := strings.TrimSpace(req.Password)
	if pwd == "" {
		pwd = strings.TrimSpace(c.FormValue("password"))
	}
	tierStr := strings.TrimSpace(req.Tier)
	if tierStr == "" {
		tierStr = strings.TrimSpace(c.FormValue("tier"))
	}

	if err := h.cfg.ValheimInstances.UpdateValheimSettings(c.UserContext(), num, name, pwd, tierStr, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Failed to update settings: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", "Settings saved successfully.", nil)
}

// ValheimInstanceModsRemove removes a mod from an instance's mods.txt.
func (h *Handler) ValheimInstanceModsRemove(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	var req struct {
		Slug string `json:"slug" form:"slug"`
	}
	_ = c.BodyParser(&req)

	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = strings.TrimSpace(c.FormValue("slug"))
	}
	if slug == "" {
		slug = strings.TrimSpace(c.Query("slug"))
	}
	if slug == "" {
		return shared.SSEToast(c, "err", "Mod slug required.", nil)
	}

	if err := h.cfg.ValheimInstances.RemoveMod(c.UserContext(), num, slug, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Remove failed: "+err.Error(), nil)
	}

	updatedMods, _ := h.cfg.ValheimInstances.GetInstalledMods(c.UserContext(), num)
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	_ = sse.InnerElement(w, "#installed-mods-container", renderInstalledModsHTML(num, updatedMods))
	_ = sse.PatchSignals(w, map[string]any{
		"toast":     fmt.Sprintf("Removed %s. Updating...", slug),
		"toastkind": "ok",
	})
	_ = w.Flush()
	return c.Send(buf.Bytes())
}

// ValheimInstanceModsInstall installs a mod onto a Valheim instance.
func (h *Handler) ValheimInstanceModsInstall(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	var req struct {
		Slug string `json:"slug" form:"slug"`
	}
	_ = c.BodyParser(&req)

	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = strings.TrimSpace(c.FormValue("slug"))
	}
	if slug == "" {
		slug = strings.TrimSpace(c.Query("slug"))
	}
	if slug == "" {
		return shared.SSEToast(c, "err", "Mod slug required.", nil)
	}

	added, err := h.cfg.ValheimInstances.InstallMod(c.UserContext(), num, slug, h.cfg.Actor(c))
	if err != nil {
		return shared.SSEToast(c, "err", "Install failed: "+err.Error(), nil)
	}

	updatedMods, _ := h.cfg.ValheimInstances.GetInstalledMods(c.UserContext(), num)
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	_ = sse.InnerElement(w, "#installed-mods-container", renderInstalledModsHTML(num, updatedMods))
	_ = sse.PatchSignals(w, map[string]any{
		"toast":     fmt.Sprintf("Installed %s (+%d mods). Updating...", slug, added),
		"toastkind": "ok",
	})
	_ = w.Flush()
	return c.Send(buf.Bytes())
}

// ValheimInstanceModsSearch searches Thunderstore packages for a Valheim instance.
func (h *Handler) ValheimInstanceModsSearch(c *fiber.Ctx) error {
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}

	var req struct {
		Q        string `json:"q" form:"q"`
		ModQuery string `json:"modQuery" form:"modQuery"`
	}
	_ = c.BodyParser(&req)

	q := strings.TrimSpace(req.ModQuery)
	if q == "" {
		q = strings.TrimSpace(req.Q)
	}
	if q == "" {
		q = strings.TrimSpace(c.Query("q"))
	}

	if q == "" {
		return ssePatchElements(c, "#valheim-mod-results", `<p class="text-xs text-zinc-500 py-3 text-center">Type a search term above (e.g. mining, jotunn, inventory).</p>`)
	}

	if h.cfg.TS == nil {
		return ssePatchElements(c, "#valheim-mod-results", `<p class="text-xs text-red-400 py-3 text-center">Thunderstore catalog unconfigured.</p>`)
	}

	results, err := h.cfg.TS.Search(c.UserContext(), q, 15)
	if err != nil {
		return ssePatchElements(c, "#valheim-mod-results", fmt.Sprintf(`<p class="text-xs text-red-400 py-3 text-center">Search failed: %s</p>`, html.EscapeString(err.Error())))
	}
	if len(results) == 0 {
		return ssePatchElements(c, "#valheim-mod-results", `<p class="text-xs text-zinc-500 py-4 text-center">No Thunderstore mods found matching your search.</p>`)
	}

	var sb strings.Builder
	for _, r := range results {
		fullName := fmt.Sprintf("%s-%s", r.Owner, r.Name)
		cleanSlug := strings.ReplaceAll(fullName, "'", "\\'")
		cleanIcon := r.Icon
		if cleanIcon == "" {
			cleanIcon = "/assets/img/valheim-icon.png"
		}
		sb.WriteString(fmt.Sprintf(`
			<div class="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950 p-3 hover:border-zinc-700 transition">
				<div class="flex items-center gap-3 overflow-hidden min-w-0 flex-1 mr-3">
					<img src="%s" alt="" class="h-8 w-8 rounded-lg bg-zinc-800 object-cover shrink-0" onerror="this.style.display='none'"/>
					<div class="truncate">
						<div class="flex items-center gap-2">
							<span class="text-xs font-semibold text-zinc-200">%s</span>
							<span class="font-mono text-[10px] text-orange-400 bg-orange-950/60 border border-orange-800/40 rounded px-1.5 py-0.5">%s</span>
							<span class="font-mono text-[10px] text-zinc-500">v%s</span>
						</div>
						<p class="truncate text-[11px] text-zinc-400 mt-0.5">%s</p>
					</div>
				</div>
				<button type="button"
					data-on:click="@post('/api/valheim/%d/mods/install', {slug: '%s'})"
					class="shrink-0 rounded-lg bg-orange-700 px-3 py-1.5 text-xs font-medium text-white hover:bg-orange-600 transition shadow-sm">
					+ Install
				</button>
			</div>
		`, html.EscapeString(cleanIcon), html.EscapeString(r.Name), html.EscapeString(fullName), html.EscapeString(r.Version), html.EscapeString(r.Description), num, cleanSlug))
	}

	return ssePatchElements(c, "#valheim-mod-results", sb.String())
}

func ssePatchElements(c *fiber.Ctx, selector, content string) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := sse.InnerElement(w, selector, content); err != nil {
		return err
	}
	return c.Send(buf.Bytes())
}

func renderInstalledModsHTML(num int, mods []string) string {
	if len(mods) == 0 {
		return `<div class="p-8 text-center text-xs text-zinc-500">No custom mods installed in this world. Server runs clean BepInEx.</div>`
	}
	var sb strings.Builder
	sb.WriteString(`<table class="w-full text-left text-xs">`)
	sb.WriteString(`<thead class="border-b border-zinc-800 bg-zinc-950/60 text-zinc-400"><tr><th class="px-4 py-2.5 font-medium">Mod Package</th><th class="px-4 py-2.5 font-medium text-right">Actions</th></tr></thead>`)
	sb.WriteString(`<tbody class="divide-y divide-zinc-800/60">`)
	for _, modSlug := range mods {
		cleanSlug := html.EscapeString(modSlug)
		jsSlug := strings.ReplaceAll(modSlug, "'", "\\'")
		sb.WriteString(fmt.Sprintf(`<tr class="hover:bg-zinc-800/30"><td class="px-4 py-2.5 font-mono text-zinc-200">%s</td><td class="px-4 py-2.5 text-right"><button type="button" data-on:click="@post('/api/valheim/%d/mods/remove', {slug: '%s'})" class="text-red-400 hover:text-red-300">Remove</button></td></tr>`, cleanSlug, num, jsSlug))
	}
	sb.WriteString(`</tbody></table>`)
	return sb.String()
}

// ValheimInstanceExport exports the instance client bundle as an .r2z modpack profile.
func (h *Handler) ValheimInstanceExport(c *fiber.Ctx) error {
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}

	if h.cfg.ValheimInstances == nil || h.cfg.ValheimGame == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Export service unavailable")
	}

	inst, err := h.cfg.ValheimInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Valheim instance not found")
	}

	bundle, err := h.cfg.ValheimGame.ExportClientBundle(c.UserContext(), *inst)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Build .r2z profile failed: " + err.Error())
	}

	c.Set("Content-Type", bundle.ContentType)
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, bundle.Filename))
	return c.Send(bundle.Data)
}
