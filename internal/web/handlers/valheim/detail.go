package valheim

import (
	"bufio"
	"bytes"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/domain"
	"agrelha/internal/web/mdrender"
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
	d.Vanilla = inst.IsVanilla()

	if h.cfg.LastIncident != nil {
		if in, err := h.cfg.LastIncident(c.UserContext(), inst.Number); err == nil {
			d.LastIncident = pages.IncidentView(in)
		}
	}

	// Fetch installed mods if on mods tab or overview
	if tab == "mods" || tab == "overview" {
		if mods, err := h.cfg.ValheimInstances.GetInstalledMods(c.UserContext(), inst.Number); err == nil {
			d.InstalledMods = mods
		}
	}

	if tab == "mods" && !d.Vanilla {
		h.ModUpdateState(c.UserContext(), &d, *inst)
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
		Q         string `json:"q" form:"q"`
		ModQuery  string `json:"modQuery" form:"modQuery"`
		ModSearch string `json:"modSearch" form:"modSearch"`
		Limit     int    `json:"limit" form:"limit"`
	}
	_ = c.BodyParser(&req)

	q := strings.TrimSpace(req.ModSearch)
	if q == "" {
		q = strings.TrimSpace(req.ModQuery)
	}
	if q == "" {
		q = strings.TrimSpace(req.Q)
	}
	if q == "" {
		q = strings.TrimSpace(c.Query("q"))
	}
	limit := req.Limit
	if limit <= 0 {
		if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 {
			limit = l
		} else {
			limit = 24
		}
	}

	if h.cfg.TS == nil {
		return ssePatchElements(c, "#valheim-mod-results", `<div class="col-span-full py-8 text-center text-xs text-red-400">Thunderstore catalog unconfigured.</div>`)
	}

	results, err := h.cfg.TS.Search(c.UserContext(), q, limit)
	if err != nil {
		return ssePatchElements(c, "#valheim-mod-results", fmt.Sprintf(`<div class="col-span-full py-8 text-center text-xs text-red-400">Search failed: %s</div>`, html.EscapeString(err.Error())))
	}
	if len(results) == 0 {
		return ssePatchElements(c, "#valheim-mod-results", `<div class="col-span-full py-12 text-center text-xs text-zinc-500">No Thunderstore mods found matching your search.</div>`)
	}

	installedMap := make(map[string]bool)
	if h.cfg.ValheimInstances != nil {
		if installed, err := h.cfg.ValheimInstances.GetInstalledMods(c.UserContext(), num); err == nil {
			for _, m := range installed {
				// Entries may be pinned ("Ns/Name/1.2.0") or bare ("Ns-Name");
				// both are the same mod, so compare on identity.
				if ref, ok := domain.ParseModRef(m, domain.GameValheim); ok {
					installedMap[ref.Key()] = true
				}
			}
		}
	}

	var sb strings.Builder
	if q == "" {
		sb.WriteString(`<div class="col-span-full flex items-center justify-between pb-1 text-xs text-zinc-400 font-medium"><span>Popular Community Mods on Thunderstore</span><span class="text-zinc-500">Ranked by downloads</span></div>`)
	} else {
		sb.WriteString(fmt.Sprintf(`<div class="col-span-full flex items-center justify-between pb-1 text-xs text-zinc-400 font-medium"><span>Search Results for "%s" (%d)</span></div>`, html.EscapeString(q), len(results)))
	}

	for _, r := range results {
		fullName := fmt.Sprintf("%s-%s", r.Owner, r.Name)
		cleanSlug := strings.ReplaceAll(fullName, "'", "\\'")
		cleanIcon := r.Icon
		if cleanIcon == "" {
			cleanIcon = "/assets/img/valheim-icon.png"
		}
		ref, _ := domain.ParseModRef(fullName, domain.GameValheim)
		isInstalled := installedMap[ref.Key()]

		var statusBadge string
		var actionBtn string
		if isInstalled {
			statusBadge = `<span class="rounded bg-emerald-950/70 border border-emerald-800/60 px-1.5 py-0.5 text-[10px] font-medium text-emerald-300">Installed</span>`
			actionBtn = fmt.Sprintf(`<button type="button" data-on:click="@post('/api/valheim/%d/mods/remove?slug=%s', {payload: {slug: '%s'}})" class="rounded-lg bg-red-950/50 border border-red-800/60 px-3 py-1.5 text-xs font-medium text-red-300 hover:bg-red-900/60 transition">Remove</button>`, num, cleanSlug, cleanSlug)
		} else {
			actionBtn = fmt.Sprintf(`<button type="button" data-on:click="@post('/api/valheim/%d/mods/install?slug=%s', {payload: {slug: '%s'}})" class="rounded-lg bg-zinc-100 px-3.5 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white transition shadow-sm">+ Install</button>`, num, cleanSlug, cleanSlug)
		}

		sb.WriteString(fmt.Sprintf(`
			<div class="flex flex-col justify-between rounded-xl border border-zinc-800 bg-zinc-950 p-4 hover:border-zinc-700 transition shadow-sm">
				<div>
					<div class="flex items-start gap-3">
						<img src="%s" alt="" class="h-10 w-10 rounded-lg bg-zinc-800 object-cover shrink-0 mt-0.5" onerror="this.style.display='none'"/>
						<div class="min-w-0 flex-1 truncate">
							<div class="flex items-center gap-1.5 flex-wrap">
								<span class="text-sm font-semibold text-zinc-100 truncate">%s</span>
								<span class="font-mono text-[10px] text-zinc-400 bg-zinc-800/80 border border-zinc-700/60 rounded px-1.5 py-0.5">%s</span>
								<span class="font-mono text-[10px] text-zinc-500">v%s</span>
							</div>
							<div class="flex items-center gap-2 mt-1 text-[11px] text-zinc-400">
								<span>%s downloads</span>
								%s
							</div>
						</div>
					</div>
					<p class="mt-3 line-clamp-2 text-xs text-zinc-400 leading-relaxed">%s</p>
				</div>
				<div class="mt-4 flex items-center justify-between border-t border-zinc-800/60 pt-3">
					<button type="button"
						data-on:click="@post('/api/valheim/%d/mods/detail', {payload: {slug: '%s'}})"
						class="rounded-lg bg-zinc-800 border border-zinc-700/80 px-3 py-1.5 text-xs font-medium text-zinc-300 hover:bg-zinc-700 hover:text-white transition">
						📖 Details
					</button>
					%s
				</div>
			</div>
		`, html.EscapeString(cleanIcon), html.EscapeString(r.Name), html.EscapeString(r.Owner), html.EscapeString(r.Version), formatDownloads(r.Downloads), statusBadge, html.EscapeString(r.Description), num, cleanSlug, actionBtn))
	}

	if len(results) >= limit {
		cleanQ := strings.ReplaceAll(q, "'", "\\'")
		sb.WriteString(fmt.Sprintf(`
			<div class="col-span-full pt-2 pb-2 text-center">
				<button type="button" data-on:click="@post('/api/valheim/%d/mods/search', {payload: {q: '%s', limit: %d}})" class="rounded-xl border border-zinc-700 bg-zinc-800 px-5 py-2 text-xs font-medium text-zinc-200 hover:bg-zinc-700 hover:text-white transition shadow-sm">
					Load more mods...
				</button>
			</div>
		`, num, cleanQ, limit+24))
	}

	return ssePatchElements(c, "#valheim-mod-results", sb.String())
}

// ValheimModDetail renders the slide-over detail drawer with README, versions, and dependencies via SSE.
func (h *Handler) ValheimModDetail(c *fiber.Ctx) error {
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
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

	ctx := c.UserContext()
	owner, name := parseSlug(slug)

	var entry domain.ModSearchResult
	if h.cfg.TS != nil {
		entry, _ = h.cfg.TS.Get(owner + "/" + name)
		if entry.Owner == "" {
			entry, _ = h.cfg.TS.Get(slug)
		}
	}
	if entry.Owner == "" {
		entry.Owner = owner
	}
	if entry.Name == "" {
		entry.Name = name
	}

	version := entry.Version
	var depRaw []string
	if h.cfg.TS != nil {
		if v, deps, err := h.cfg.TS.LatestVersion(ctx, owner, name); err == nil {
			if version == "" {
				version = v
			}
			depRaw = deps
		}
	}

	var readmeHTML string
	if version != "" {
		key := owner + "/" + name
		var md string
		var hit bool
		if h.cfg.ReadmeCache != nil {
			md, hit, _ = h.cfg.ReadmeCache.GetReadme(key, version)
		}
		if !hit && h.cfg.TS != nil {
			if fetched, err := h.cfg.TS.Readme(ctx, owner, name, version); err == nil {
				md = fetched
				if h.cfg.ReadmeCache != nil {
					_ = h.cfg.ReadmeCache.PutReadme(key, version, md)
				}
			}
		}
		if md != "" {
			readmeHTML = mdrender.Render(md)
		}
	}

	isInstalled := false
	if h.cfg.ValheimInstances != nil {
		if installed, err := h.cfg.ValheimInstances.GetInstalledMods(ctx, num); err == nil {
			for _, m := range installed {
				cleanM := strings.ToLower(strings.TrimSpace(m))
				if cleanM == strings.ToLower(slug) || cleanM == strings.ToLower(owner+"-"+name) || cleanM == strings.ToLower(name) {
					isInstalled = true
					break
				}
			}
		}
	}

	fullSlug := fmt.Sprintf("%s-%s", owner, name)
	jsSlug := strings.ReplaceAll(fullSlug, "'", "\\'")
	icon := entry.Icon
	if icon == "" {
		icon = "/assets/img/valheim-icon.png"
	}
	tsURL := entry.FullURL
	if tsURL == "" {
		tsURL = fmt.Sprintf("https://thunderstore.io/c/valheim/p/%s/%s/", owner, name)
	}

	deps := prettyDeps(depRaw)
	var depsHTML string
	if len(deps) > 0 {
		var dsb strings.Builder
		dsb.WriteString(`<div class="space-y-2"><h4 class="text-xs font-semibold uppercase tracking-wider text-zinc-400">Required Dependencies</h4><div class="flex flex-wrap gap-2">`)
		for _, dep := range deps {
			dsb.WriteString(fmt.Sprintf(`<span class="rounded-lg bg-zinc-800 border border-zinc-700 px-2.5 py-1 font-mono text-xs text-zinc-300">%s</span>`, html.EscapeString(dep)))
		}
		dsb.WriteString(`</div></div>`)
		depsHTML = dsb.String()
	}

	var actionBtn string
	if isInstalled {
		actionBtn = fmt.Sprintf(`<button type="button" data-on:click="@post('/api/valheim/%d/mods/remove?slug=%s', {payload: {slug: '%s'}}); $showModDetail = false" class="rounded-lg bg-red-950/60 border border-red-800/60 px-4 py-2 text-xs font-medium text-red-300 hover:bg-red-900/60 transition">Remove Mod</button>`, num, jsSlug, jsSlug)
	} else {
		actionBtn = fmt.Sprintf(`<button type="button" data-on:click="@post('/api/valheim/%d/mods/install?slug=%s', {payload: {slug: '%s'}}); $showModDetail = false" class="rounded-lg bg-zinc-100 px-5 py-2 text-xs font-semibold text-zinc-950 hover:bg-white transition shadow-sm">+ Install Mod</button>`, num, jsSlug, jsSlug)
	}

	var readmeContent string
	if readmeHTML != "" {
		readmeContent = fmt.Sprintf(`<div class="prose prose-invert prose-sm max-w-none text-zinc-300 leading-relaxed overflow-x-auto">%s</div>`, readmeHTML)
	} else {
		readmeContent = `<p class="text-xs text-zinc-500 py-6 text-center">No README available for this mod package.</p>`
	}

	drawerContent := fmt.Sprintf(`
		<div class="p-6 space-y-6">
			<!-- Header -->
			<div class="flex items-start justify-between gap-4">
				<div class="flex items-start gap-4 min-w-0 flex-1">
					<img src="%s" alt="" class="h-14 w-14 rounded-xl bg-zinc-800 object-cover shadow-md shrink-0 mt-0.5" onerror="this.style.display='none'"/>
					<div class="min-w-0 flex-1">
						<div class="flex items-center gap-2 flex-wrap">
							<h3 class="text-lg font-bold text-zinc-100">%s</h3>
							<span class="font-mono text-xs text-zinc-400 bg-zinc-800 border border-zinc-700 rounded px-2 py-0.5">%s</span>
							<span class="font-mono text-xs text-zinc-500">v%s</span>
						</div>
						<div class="mt-1 flex items-center gap-3 text-xs text-zinc-400">
							<span>%s downloads</span>
							<a href="%s" target="_blank" rel="noopener noreferrer" class="text-zinc-400 hover:text-zinc-200 underline">Thunderstore ↗</a>
						</div>
						<p class="mt-2 text-xs text-zinc-300 leading-relaxed">%s</p>
					</div>
				</div>
				<button type="button" data-on:click="$showModDetail = false" class="rounded-lg p-2 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100 text-lg">✕</button>
			</div>

			<!-- Actions Bar -->
			<div class="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950 p-4">
				<div>
					%s
				</div>
				%s
			</div>

			<!-- Dependencies -->
			%s

			<!-- README Content -->
			<div class="space-y-2">
				<h4 class="text-xs font-semibold uppercase tracking-wider text-zinc-400">Documentation</h4>
				<div class="rounded-xl border border-zinc-800 bg-zinc-950 p-5">
					%s
				</div>
			</div>
		</div>
	`, html.EscapeString(icon), html.EscapeString(entry.Name), html.EscapeString(entry.Owner), html.EscapeString(version), formatDownloads(entry.Downloads), html.EscapeString(tsURL), html.EscapeString(entry.Description), statusBadge(isInstalled), actionBtn, depsHTML, readmeContent)

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := sse.InnerElement(w, "#valheim-mod-detail-content", drawerContent); err != nil {
		return err
	}
	if err := sse.PatchSignals(w, map[string]any{
		"showModDetail": true,
	}); err != nil {
		return err
	}
	_ = w.Flush()
	return c.Send(buf.Bytes())
}

func parseSlug(slug string) (string, string) {
	slug = strings.TrimSpace(slug)
	if strings.Contains(slug, "/") {
		parts := strings.SplitN(slug, "/", 2)
		return parts[0], parts[1]
	}
	if strings.Contains(slug, "-") {
		parts := strings.SplitN(slug, "-", 2)
		return parts[0], parts[1]
	}
	return "denikson", slug
}

func formatDownloads(d int64) string {
	if d >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(d)/1_000_000)
	}
	if d >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(d)/1_000)
	}
	return strconv.FormatInt(d, 10)
}

func prettyDeps(raw []string) []string {
	var out []string
	for _, d := range raw {
		parts := strings.Split(d, "-")
		if len(parts) < 3 {
			continue
		}
		n := strings.Join(parts[1:len(parts)-1], "-")
		if strings.HasPrefix(strings.ToLower(n), "bepinexpack") {
			continue
		}
		out = append(out, n)
	}
	return out
}

func statusBadge(installed bool) string {
	if installed {
		return `<span class="inline-flex items-center gap-1.5 rounded-md bg-emerald-950/70 border border-emerald-800/60 px-2.5 py-1 text-xs font-medium text-emerald-300">✓ Installed in this world</span>`
	}
	return `<span class="text-xs text-zinc-400">Available to install</span>`
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
		ref, _ := domain.ParseModRef(modSlug, domain.GameValheim)
		label := ref.FullName()
		if ref.Version != "" {
			label += "  v" + ref.Version
		}
		cleanSlug := html.EscapeString(label)
		jsSlug := strings.ReplaceAll(ref.FullName(), "'", "\\'")
		sb.WriteString(fmt.Sprintf(`<tr class="hover:bg-zinc-800/30"><td class="px-4 py-2.5 font-mono text-zinc-200">%s</td><td class="px-4 py-2.5 text-right"><button type="button" data-on:click="@post('/api/valheim/%d/mods/remove?slug=%s', {payload: {slug: '%s'}})" class="text-red-400 hover:text-red-300">Remove</button></td></tr>`, cleanSlug, num, jsSlug, jsSlug))
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
