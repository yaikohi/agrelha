package valheim

import (
	"bufio"
	"bytes"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/modpack"
	"agrelha/internal/domain"
	"agrelha/internal/web/mdrender"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
)

// ValheimWizardPage renders the Valheim creation wizard page.
func (h *Handler) ValheimWizardPage(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Valheim instance manager not configured")
	}

	instances, err := h.cfg.ValheimInstances.ListInstances(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("List instances failed: " + err.Error())
	}

	budget := h.cfg.ValheimInstances.Budget(instances)
	budgetUI := pages.BudgetUI{
		UsedGiB:        budget.UsedGiB,
		TotalBudgetGiB: budget.TotalBudgetGiB,
		RunningCount:   budget.RunningCount,
		MaxRunning:     budget.MaxRunning,
		TotalInstances: budget.TotalInstances,
		MaxInstances:   budget.MaxInstances,
	}

	return shared.Render(c, pages.ValheimWizard(budgetUI))
}

// ValheimWizardModsSearch queries Thunderstore for mod packages during wizard setup.
func (h *Handler) ValheimWizardModsSearch(c *fiber.Ctx) error {
	var req struct {
		Q               string `json:"q" form:"q"`
		ModQuery        string `json:"modQuery" form:"modQuery"`
		ModSearch       string `json:"modSearch" form:"modSearch"`
		WizardModSearch string `json:"wizardModSearch" form:"wizardModSearch"`
		Limit           int    `json:"limit" form:"limit"`
	}
	_ = c.BodyParser(&req)

	q := strings.TrimSpace(req.WizardModSearch)
	if q == "" {
		q = strings.TrimSpace(req.ModSearch)
	}
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
		if strings.Contains(c.Get("Accept"), "application/json") {
			return c.JSON([]any{})
		}
		return ssePatchElements(c, "#wizard-valheim-mod-results", `<div class="col-span-full py-8 text-center text-xs text-red-400">Thunderstore catalog unconfigured.</div>`)
	}

	results, err := h.cfg.TS.Search(c.UserContext(), q, limit)
	if err != nil {
		if strings.Contains(c.Get("Accept"), "application/json") {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return ssePatchElements(c, "#wizard-valheim-mod-results", fmt.Sprintf(`<div class="col-span-full py-8 text-center text-xs text-red-400">Search failed: %s</div>`, html.EscapeString(err.Error())))
	}

	if strings.Contains(c.Get("Accept"), "application/json") {
		type hit struct {
			Name        string `json:"name"`
			Namespace   string `json:"namespace"`
			FullName    string `json:"full_name"`
			Description string `json:"description"`
			Version     string `json:"version"`
			IconURL     string `json:"icon_url"`
		}
		hits := make([]hit, 0, len(results))
		for _, r := range results {
			hits = append(hits, hit{
				Name:        r.Name,
				Namespace:   r.Owner,
				FullName:    fmt.Sprintf("%s-%s", r.Owner, r.Name),
				Description: r.Description,
				Version:     r.Version,
				IconURL:     r.Icon,
			})
		}
		return c.JSON(hits)
	}

	if len(results) == 0 {
		return ssePatchElements(c, "#wizard-valheim-mod-results", `<div class="col-span-full py-12 text-center text-xs text-zinc-500">No Thunderstore mods found matching your search.</div>`)
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
								<span data-show="$cart.includes('%s')" class="rounded bg-emerald-950/70 border border-emerald-800/60 px-1.5 py-0.5 text-[10px] font-medium text-emerald-300">In Cart</span>
							</div>
						</div>
					</div>
					<p class="mt-3 line-clamp-2 text-xs text-zinc-400 leading-relaxed">%s</p>
				</div>
				<div class="mt-4 flex items-center justify-between border-t border-zinc-800/60 pt-3">
					<button type="button"
						data-on:click="@post('/api/valheim/wizard/mods/detail', {payload: {slug: '%s'}})"
						class="rounded-lg bg-zinc-800 border border-zinc-700/80 px-3 py-1.5 text-xs font-medium text-zinc-300 hover:bg-zinc-700 hover:text-white transition">
						📖 Details
					</button>
					<div>
						<button type="button"
							data-show="!$cart.includes('%s')"
							data-on:click="if (!$cart.includes('%s')) { $cart = [...$cart, '%s']; @post('/api/valheim/wizard/cart/sync') }"
							class="rounded-lg bg-zinc-100 px-3.5 py-1.5 text-xs font-semibold text-zinc-950 hover:bg-white transition shadow-sm">
							+ Add
						</button>
						<button type="button"
							data-show="$cart.includes('%s')"
							data-on:click="$cart = $cart.filter(function(s){ return s !== '%s' }); @post('/api/valheim/wizard/cart/sync')"
							class="rounded-lg bg-red-950/50 border border-red-800/60 px-3 py-1.5 text-xs font-medium text-red-300 hover:bg-red-900/60 transition">
							Remove
						</button>
					</div>
				</div>
			</div>
		`, html.EscapeString(cleanIcon), html.EscapeString(r.Name), html.EscapeString(r.Owner), html.EscapeString(r.Version), formatDownloads(r.Downloads), cleanSlug, html.EscapeString(r.Description), cleanSlug, cleanSlug, cleanSlug, cleanSlug, cleanSlug, cleanSlug))
	}

	if len(results) >= limit {
		cleanQ := strings.ReplaceAll(q, "'", "\\'")
		sb.WriteString(fmt.Sprintf(`
			<div class="col-span-full pt-2 pb-2 text-center">
				<button type="button" data-on:click="@post('/api/valheim/wizard/mods/search', {payload: {wizardModSearch: '%s', limit: %d}})" class="rounded-xl border border-zinc-700 bg-zinc-800 px-5 py-2 text-xs font-medium text-zinc-200 hover:bg-zinc-700 hover:text-white transition shadow-sm">
					Load more mods...
				</button>
			</div>
		`, cleanQ, limit+24))
	}

	return ssePatchElements(c, "#wizard-valheim-mod-results", sb.String())
}

// renderWizardCartHTML formats selected mod chips for display in the wizard.
func renderWizardCartHTML(cart []string) string {
	var valid []string
	for _, s := range cart {
		s = strings.TrimSpace(s)
		if s != "" {
			valid = append(valid, s)
		}
	}
	if len(valid) == 0 {
		return `<span class="text-xs text-zinc-500 py-1">No mods added yet. Search or click "+ Add" on popular mods below.</span>`
	}

	var sb strings.Builder
	sb.WriteString(`<div class="flex flex-wrap gap-2">`)
	for _, slug := range valid {
		cleanSlug := strings.ReplaceAll(slug, "'", "\\'")
		sb.WriteString(fmt.Sprintf(`
			<span class="inline-flex items-center gap-1.5 rounded-lg bg-zinc-800 border border-zinc-700/80 px-2.5 py-1 text-xs font-mono text-zinc-200">
				<span>%s</span>
				<button type="button"
					data-on:click="$cart = $cart.filter(function(s){ return s !== '%s' }); @post('/api/valheim/wizard/cart/sync')"
					class="text-zinc-400 hover:text-red-400 transition ml-1"
					title="Remove from cart">✕</button>
			</span>
		`, html.EscapeString(slug), cleanSlug))
	}
	sb.WriteString(`</div>`)
	return sb.String()
}

// ValheimWizardCartSync synchronizes the selected mods array and re-renders the cart chips tray.
func (h *Handler) ValheimWizardCartSync(c *fiber.Ctx) error {
	var req struct {
		Cart []string `json:"cart" form:"cart"`
	}
	if err := c.BodyParser(&req); err != nil || len(req.Cart) == 0 {
		if raw := c.FormValue("cart"); raw != "" {
			req.Cart = strings.Split(raw, ",")
		}
	}

	var valid []string
	seen := make(map[string]bool)
	for _, s := range req.Cart {
		s = strings.TrimSpace(s)
		if s != "" && !seen[strings.ToLower(s)] {
			seen[strings.ToLower(s)] = true
			valid = append(valid, s)
		}
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	_ = innerElement(w, "#wizard-valheim-cart-items", renderWizardCartHTML(valid))
	_ = patchSignals(w, map[string]any{
		"cart": valid,
	})
	_ = w.Flush()
	return c.Send(buf.Bytes())
}

// ValheimWizardModDetail renders the slide-over detail drawer for the wizard with README and cart toggles.
func (h *Handler) ValheimWizardModDetail(c *fiber.Ctx) error {
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
				<button type="button" data-on:click="$showWizardModDetail = false" class="rounded-lg p-2 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100 text-lg">✕</button>
			</div>

			<!-- Actions Bar -->
			<div class="flex items-center justify-between rounded-xl border border-zinc-800 bg-zinc-950 p-4">
				<div>
					<span data-show="$cart.includes('%s')" class="inline-flex items-center gap-1.5 rounded-md bg-emerald-950/70 border border-emerald-800/60 px-2.5 py-1 text-xs font-medium text-emerald-300">✓ In your cart</span>
					<span data-show="!$cart.includes('%s')" class="text-xs text-zinc-400">Ready to add to world</span>
				</div>
				<div>
					<button type="button"
						data-show="!$cart.includes('%s')"
						data-on:click="if (!$cart.includes('%s')) { $cart = [...$cart, '%s'] }; @post('/api/valheim/wizard/cart/sync'); $showWizardModDetail = false"
						class="rounded-lg bg-zinc-100 px-5 py-2 text-xs font-semibold text-zinc-950 hover:bg-white transition shadow-sm">
						+ Add to Cart
					</button>
					<button type="button"
						data-show="$cart.includes('%s')"
						data-on:click="$cart = $cart.filter(function(s){ return s !== '%s' }); @post('/api/valheim/wizard/cart/sync'); $showWizardModDetail = false"
						class="rounded-lg bg-red-950/60 border border-red-800/60 px-4 py-2 text-xs font-medium text-red-300 hover:bg-red-900/60 transition">
						Remove from Cart
					</button>
				</div>
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
	`, html.EscapeString(icon), html.EscapeString(entry.Name), html.EscapeString(entry.Owner), html.EscapeString(version), formatDownloads(entry.Downloads), html.EscapeString(tsURL), html.EscapeString(entry.Description), jsSlug, jsSlug, jsSlug, jsSlug, jsSlug, jsSlug, jsSlug, depsHTML, readmeContent)

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := innerElement(w, "#wizard-valheim-mod-detail-content", drawerContent); err != nil {
		return err
	}
	if err := patchSignals(w, map[string]any{
		"showWizardModDetail": true,
	}); err != nil {
		return err
	}
	_ = w.Flush()
	return c.Send(buf.Bytes())
}

// ValheimWizardImport handles uploading an .r2z or export.r2x modpack profile.
func (h *Handler) ValheimWizardImport(c *fiber.Ctx) error {
	fh, err := c.FormFile("file")
	if err != nil {
		return shared.SSEToast(c, "err", "File upload failed: "+err.Error(), map[string]any{
			"importError": err.Error(),
		})
	}

	f, err := openFormFile(fh)
	if err != nil {
		return shared.SSEToast(c, "err", "Failed to open uploaded file: "+err.Error(), map[string]any{
			"importError": err.Error(),
		})
	}
	defer f.Close()

	buf, err := readFormFile(f)
	if err != nil {
		return shared.SSEToast(c, "err", "Read upload failed: "+err.Error(), map[string]any{
			"importError": err.Error(),
		})
	}

	imported, err := modpack.ParseR2Z(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		return shared.SSEToast(c, "err", "Failed to parse modpack profile: "+err.Error(), map[string]any{
			"importError": err.Error(),
		})
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	var respBuf bytes.Buffer
	w := bufio.NewWriter(&respBuf)
	_ = innerElement(w, "#wizard-valheim-cart-items", renderWizardCartHTML(imported.Slugs))

	signals := map[string]any{
		"toast":       fmt.Sprintf("Imported %d mods from profile!", len(imported.Slugs)),
		"toastkind":   "ok",
		"cart":        imported.Slugs,
		"raw_mods":    imported.RawMods,
		"source":      "scratch",
		"importError": "",
	}
	if imported.Name != "" {
		signals["name"] = imported.Name
	}
	_ = patchSignals(w, signals)
	_ = w.Flush()
	return c.Send(respBuf.Bytes())
}

// ValheimWizardCreate handles the final submission of the Valheim creation wizard.
func (h *Handler) ValheimWizardCreate(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager unconfigured.", nil)
	}

	var req struct {
		Name     string   `json:"name" form:"name"`
		Password string   `json:"password" form:"password"`
		Seed     string   `json:"seed" form:"seed"`
		Tier     string   `json:"tier" form:"tier"`
		Source   string   `json:"source" form:"source"`
		RawMods  string   `json:"raw_mods" form:"raw_mods"`
		Mods     []string `json:"mods"`
		Cart     []string `json:"cart"`
	}
	_ = c.BodyParser(&req)

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.TrimSpace(c.FormValue("name"))
	}
	if name == "" {
		return shared.SSEToast(c, "err", "World name is required.", nil)
	}

	password := strings.TrimSpace(req.Password)
	if password == "" {
		password = strings.TrimSpace(c.FormValue("password"))
	}
	if password != "" && len(password) < 5 {
		return shared.SSEToast(c, "err", "Valheim server password must be at least 5 characters.", nil)
	}

	seed := strings.TrimSpace(req.Seed)
	if seed == "" {
		seed = strings.TrimSpace(c.FormValue("seed"))
	}

	tierStr := strings.TrimSpace(req.Tier)
	if tierStr == "" {
		tierStr = strings.TrimSpace(c.FormValue("tier"))
	}
	tier := domain.NormalizeTier(tierStr)

	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = strings.TrimSpace(c.FormValue("source"))
	}

	rawMods := req.RawMods
	if rawMods == "" {
		rawMods = c.FormValue("raw_mods")
	}

	var modsTxt string
	if source == "scratch" || source == "modpack" || len(req.Cart) > 0 {
		var combined []string
		if len(req.Cart) > 0 {
			combined = append(combined, req.Cart...)
		}
		if len(req.Mods) > 0 {
			combined = append(combined, req.Mods...)
		}
		if rawMods != "" {
			for line := range strings.SplitSeq(rawMods, "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					combined = append(combined, line)
				}
			}
		}
		seen := make(map[string]bool)
		var deduped []string
		for _, s := range combined {
			s = strings.TrimSpace(s)
			if s != "" && !seen[strings.ToLower(s)] {
				seen[strings.ToLower(s)] = true
				deduped = append(deduped, s)
			}
		}
		modsTxt = strings.Join(deduped, "\n")
	}

	// Vanilla means no BepInEx, which is the only way achievements stay earnable
	// and is immutable afterwards. Every other path installs the loader.
	instSource := domain.SourceModlist
	if source == "vanilla" && modsTxt == "" {
		instSource = domain.SourceVanilla
	}

	inst := domain.Instance{
		GameID:   domain.GameValheim,
		Name:     name,
		Password: password,
		Seed:     seed,
		Tier:     tier,
		Source:   instSource,
		State:    domain.StateRunning,
	}

	created, err := h.cfg.ValheimInstances.CreateInstance(c.UserContext(), inst, modsTxt, h.cfg.Actor(c))
	if err != nil {
		return shared.SSEToast(c, "err", "Failed to create Valheim server: "+err.Error(), nil)
	}

	_ = h.cfg.ValheimInstances.StartInstance(c.UserContext(), created.Number, h.cfg.Actor(c))

	redirectURL := fmt.Sprintf("/valheim/%d", created.Number)
	return shared.SSEToast(c, "ok", fmt.Sprintf("Created Valheim server %q! Redirecting...", created.Name), map[string]any{
		"redirect": redirectURL,
	})
}
