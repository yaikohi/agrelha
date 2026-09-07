package server

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/cmd/web/pages"
	"agrelha/internal/mcversions"
	"agrelha/internal/minecraft"
)

func (s *FiberServer) mcWizardPage(c *fiber.Ctx) error {
	if s.mcInstances == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Instance manager not configured")
	}

	instances, err := s.mcInstances.ListInstances(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("List instances failed: " + err.Error())
	}

	budget := s.mcInstances.Budget(instances)
	budgetUI := pages.BudgetUI{
		UsedGiB:        budget.UsedGiB,
		TotalBudgetGiB: budget.TotalBudgetGiB,
		RunningCount:   budget.RunningCount,
		MaxRunning:     budget.MaxRunning,
		TotalInstances: budget.TotalInstances,
		MaxInstances:   budget.MaxInstances,
	}

	var releases []string
	if s.mcv != nil {
		releases = s.mcv.Releases(c.UserContext(), 15)
	}
	if len(releases) == 0 {
		releases = []string{mcversions.FallbackLatest, "1.21.1", "1.20.1"}
	}

	return render(c, pages.MinecraftWizard(releases, budgetUI))
}

func (s *FiberServer) mcWizardModpacksSearch(c *fiber.Ctx) error {
	if s.mpi == nil {
		return c.SendString(`<div class="col-span-full text-xs text-red-400">Modpack Index unconfigured</div>`)
	}

	q := strings.TrimSpace(c.Query("q"))
	res, err := s.mpi.SearchModpacks(c.UserContext(), q, "", 1)
	if err != nil {
		return c.SendString(fmt.Sprintf(`<div class="col-span-full text-xs text-red-400">Search error: %s</div>`, err.Error()))
	}
	if len(res.Data) == 0 {
		return c.SendString(`<div class="col-span-full text-xs text-zinc-500 py-6 text-center">No modpacks found matching your search.</div>`)
	}

	var sb strings.Builder
	for _, p := range res.Data {
		cleanName := strings.ReplaceAll(p.Name, "'", "\\'")
		sb.WriteString(fmt.Sprintf(`
			<div class="flex flex-col justify-between rounded-xl border border-zinc-800 bg-zinc-950 p-4">
				<div>
					<div class="flex items-center gap-3">
						<img src="%s" alt="" class="h-10 w-10 rounded-lg bg-zinc-800 object-cover" onerror="this.style.display='none'"/>
						<div>
							<h4 class="text-sm font-semibold text-zinc-100">%s</h4>
							<p class="text-[10px] text-zinc-400">%d downloads</p>
						</div>
					</div>
					<p class="mt-2 line-clamp-2 text-xs text-zinc-400">%s</p>
				</div>
				<div class="mt-4 flex justify-end">
					<button type="button"
						data-on:click="$pack_name = '%s'; $pack_id = '%d'; $pack_ref = '%s'; $pack_provider = 'curseforge'; $step = 4"
						class="rounded-lg bg-emerald-700 px-3 py-1.5 text-xs font-medium text-white hover:bg-emerald-600">
						Select Pack
					</button>
				</div>
			</div>
		`, p.ThumbnailURL, p.Name, p.DownloadCount, p.Summary, cleanName, p.ID, p.PageURL))
	}

	c.Set("Content-Type", "text/html")
	return c.SendString(sb.String())
}

func (s *FiberServer) mcWizardModsSearch(c *fiber.Ctx) error {
	if s.mr == nil {
		return c.SendString(`<p class="text-xs text-red-400">Modrinth client unconfigured</p>`)
	}

	q := strings.TrimSpace(c.Query("q"))
	mcVersion := strings.TrimSpace(c.Query("version"))
	if mcVersion == "" {
		mcVersion = "1.21.1"
	}

	res, err := s.mr.Search(c.UserContext(), q, mcVersion, "", 20, 0)
	if err != nil {
		return c.SendString(fmt.Sprintf(`<p class="text-xs text-red-400">Search error: %s</p>`, err.Error()))
	}
	if len(res.Hits) == 0 {
		return c.SendString(`<p class="text-xs text-zinc-500">No mods found.</p>`)
	}

	var sb strings.Builder
	for _, h := range res.Hits {
		cleanSlug := strings.ReplaceAll(h.Slug, "'", "\\'")
		cleanTitle := strings.ReplaceAll(h.Title, "'", "\\'")
		sb.WriteString(fmt.Sprintf(`
			<div class="flex items-center justify-between rounded-lg border border-zinc-800 bg-zinc-950 p-2.5">
				<div class="flex items-center gap-2.5 overflow-hidden">
					<img src="%s" alt="" class="h-7 w-7 rounded bg-zinc-800 object-cover" onerror="this.style.display='none'"/>
					<div class="truncate">
						<div class="flex items-center gap-1.5">
							<span class="text-xs font-medium text-zinc-200">%s</span>
							<span class="font-mono text-[10px] text-zinc-500">%s</span>
						</div>
						<p class="truncate text-[10px] text-zinc-400">%s</p>
					</div>
				</div>
				<button type="button"
					data-on:click="if (!$cart.includes('%s')) { $cart.push('%s'); @post('/api/minecraft/wizard/cart/check') }"
					class="shrink-0 rounded bg-zinc-800 px-2.5 py-1 text-xs font-medium text-zinc-300 hover:bg-emerald-700 hover:text-white">
					+ Add
				</button>
			</div>
		`, h.IconURL, h.Title, h.Slug, h.Description, cleanSlug, cleanSlug))
		_ = cleanTitle
	}

	c.Set("Content-Type", "text/html")
	return c.SendString(sb.String())
}

func (s *FiberServer) mcWizardCartCheck(c *fiber.Ctx) error {
	type cartReq struct {
		Cart      []string `json:"cart"`
		MCVersion string   `json:"mc_version"`
	}
	var req cartReq
	if err := c.BodyParser(&req); err != nil {
		req.Cart = strings.Split(c.FormValue("cart"), ",")
		req.MCVersion = c.FormValue("mc_version")
	}

	compat := minecraft.CheckCartCompatibility(c.UserContext(), s.mr, req.Cart, req.MCVersion)

	var cartHTML strings.Builder
	for _, slug := range req.Cart {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		cleanSlug := strings.ReplaceAll(slug, "'", "\\'")
		cartHTML.WriteString(fmt.Sprintf(`
			<div class="flex items-center justify-between rounded border border-zinc-800 bg-zinc-950 px-2 py-1">
				<span class="font-mono text-zinc-300">%s</span>
				<button type="button"
					data-on:click="$cart = $cart.filter(function(s){ return s !== '%s' }); @post('/api/minecraft/wizard/cart/check')"
					class="text-zinc-500 hover:text-red-400">✕</button>
			</div>
		`, slug, cleanSlug))
	}

	return ssePatch(c, map[string]any{
		"bestLoader":   compat.BestLoader,
		"neoFit":       compat.NeoForgeFit,
		"fabFit":       compat.FabricFit,
		"totalFitMods": compat.TotalMods,
	}, map[string]string{
		"#wizard-cart-items": cartHTML.String(),
	})
}

func (s *FiberServer) mcWizardCreate(c *fiber.Ctx) error {
	if s.mcInstances == nil {
		return sseToast(c, "err", "Instance manager not configured.", nil)
	}

	var req struct {
		Name         string `json:"name" form:"name"`
		Source       string `json:"source" form:"source"`
		Loader       string `json:"loader" form:"loader"`
		MCVersion    string `json:"mc_version" form:"mc_version"`
		Tier         string `json:"tier" form:"tier"`
		Seed         string `json:"seed" form:"seed"`
		MOTD         string `json:"motd" form:"motd"`
		Difficulty   string `json:"difficulty" form:"difficulty"`
		Gamemode     string `json:"gamemode" form:"gamemode"`
		WorldType    string `json:"world_type" form:"world_type"`
		PackName     string `json:"pack_name" form:"pack_name"`
		PackRef      string `json:"pack_ref" form:"pack_ref"`
		PackProvider string `json:"pack_provider" form:"pack_provider"`
		RawMods      string `json:"raw_mods" form:"raw_mods"`
		Cart         any    `json:"cart" form:"cart"`
	}
	_ = c.BodyParser(&req)

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.TrimSpace(c.FormValue("name"))
	}
	if name == "" {
		return sseToast(c, "err", "World name is required.", nil)
	}

	source := strings.ToLower(strings.TrimSpace(req.Source))
	if source == "" {
		source = strings.ToLower(strings.TrimSpace(c.FormValue("source")))
	}
	loader := strings.ToLower(strings.TrimSpace(req.Loader))
	if loader == "" {
		loader = strings.ToLower(strings.TrimSpace(c.FormValue("loader")))
	}
	mcVersion := strings.TrimSpace(req.MCVersion)
	if mcVersion == "" {
		mcVersion = strings.TrimSpace(c.FormValue("mc_version"))
	}
	if mcVersion == "" {
		mcVersion = "1.21.1"
	}
	tierStr := req.Tier
	if tierStr == "" {
		tierStr = c.FormValue("tier")
	}
	tier := minecraft.NormalizeTier(tierStr)
	seed := strings.TrimSpace(req.Seed)
	if seed == "" {
		seed = strings.TrimSpace(c.FormValue("seed"))
	}
	motd := strings.TrimSpace(req.MOTD)
	if motd == "" {
		motd = strings.TrimSpace(c.FormValue("motd"))
	}
	difficulty := strings.TrimSpace(req.Difficulty)
	if difficulty == "" {
		difficulty = strings.TrimSpace(c.FormValue("difficulty"))
	}
	gamemode := strings.TrimSpace(req.Gamemode)
	if gamemode == "" {
		gamemode = strings.TrimSpace(c.FormValue("gamemode"))
	}
	worldType := strings.TrimSpace(req.WorldType)
	if worldType == "" {
		worldType = strings.TrimSpace(c.FormValue("world_type"))
	}

	packName := strings.TrimSpace(req.PackName)
	if packName == "" {
		packName = strings.TrimSpace(c.FormValue("pack_name"))
	}
	packRef := strings.TrimSpace(req.PackRef)
	if packRef == "" {
		packRef = strings.TrimSpace(c.FormValue("pack_ref"))
	}
	packProvider := strings.TrimSpace(req.PackProvider)
	if packProvider == "" {
		packProvider = strings.TrimSpace(c.FormValue("pack_provider"))
	}

	var modsTxt string
	rawMods := req.RawMods
	if rawMods == "" {
		rawMods = c.FormValue("raw_mods")
	}

	var cartList []string
	switch v := req.Cart.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				cartList = append(cartList, s)
			}
		}
	case []string:
		cartList = v
	case string:
		if strings.TrimSpace(v) != "" {
			cartList = strings.Split(v, ",")
		}
	}
	if len(cartList) == 0 {
		cStr := c.FormValue("cart")
		if strings.TrimSpace(cStr) != "" {
			cartList = strings.Split(cStr, ",")
		}
	}

	if strings.TrimSpace(rawMods) != "" {
		modsTxt = rawMods
	} else if len(cartList) > 0 {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("# Mod list for %s\n", name))
		for _, item := range cartList {
			item = strings.TrimSpace(item)
			if item != "" {
				sb.WriteString(item + "\n")
			}
		}
		modsTxt = sb.String()
	}

	inst := minecraft.Instance{
		Name:       name,
		Seed:       seed,
		MCVersion:  mcVersion,
		Tier:       tier,
		MOTD:       motd,
		Difficulty: difficulty,
		Gamemode:   gamemode,
		WorldType:  worldType,
	}

	switch source {
	case "vanilla":
		inst.Source = minecraft.SourceVanilla
		inst.Loader = ""
	case "modpack":
		inst.Source = minecraft.SourceModpack
		inst.Loader = minecraft.NormalizeLoader(loader)
		if packName != "" || packRef != "" {
			provider := minecraft.ProviderCurseForge
			if packProvider == "modrinth" {
				provider = minecraft.ProviderModrinth
			}
			inst.Pack = &minecraft.Pack{
				Name:     packName,
				Ref:      packRef,
				Provider: provider,
			}
		}
	default:
		inst.Source = minecraft.SourceModlist
		inst.Loader = minecraft.NormalizeLoader(loader)
	}

	created, err := s.mcInstances.CreateInstance(c.UserContext(), inst, modsTxt)
	if err != nil {
		return sseToast(c, "err", "Failed to create world: "+err.Error(), nil)
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-instance-create", fmt.Sprintf("World #%02d %q", created.Number, created.Name))
	_ = s.store.RecordEvent("mc-instance-create", s.actor(c))

	// Attempt to start the newly created instance immediately
	_ = s.mcInstances.StartInstance(c.UserContext(), created.Number)

	redirectURL := fmt.Sprintf("/minecraft/provisioning/%d", created.Number)
	return sseToast(c, "ok", fmt.Sprintf("Created world %q! Redirecting...", created.Name), map[string]any{
		"redirect": redirectURL,
	})
}

func (s *FiberServer) mcProvisioningPage(c *fiber.Ctx) error {
	if s.mcInstances == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Instance manager not configured")
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}

	inst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Instance not found")
	}

	instUI := pages.InstanceUI{
		Number:    inst.Number,
		Name:      inst.Name,
		Slug:      inst.Slug,
		Seed:      inst.Seed,
		Loader:    string(inst.Loader),
		Source:    string(inst.Source),
		MCVersion: inst.MCVersion,
		Tier:      string(inst.Tier),
		MemoryGiB: inst.MemoryGiB(),
		State:     string(inst.State),
		MOTD:      inst.MOTD,
		LBIP:      inst.LBIP,
	}

	return render(c, pages.MinecraftProvisioning(instUI))
}

func (s *FiberServer) mcProvisioningStream(c *fiber.Ctx) error {
	if s.mcInstances == nil || s.mck8s == nil {
		return c.SendString("data: {phase:'ready',ready:true}\n\n")
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid number")
	}

	inst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Not found")
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		depName := inst.DeploymentName()
		for i := 0; i < 60; i++ {
			desired, ready, err := s.mck8s.DeploymentReplicas(context.Background(), depName)
			phase := "syncing"
			isReady := false

			if err == nil {
				if ready > 0 {
					phase = "ready"
					isReady = true
				} else if desired > 0 {
					phase = "booting"
				}
			}

			payload := fmt.Sprintf(`{"phase":%q,"ready":%t,"lb_ip":%q,"num":%d}`, phase, isReady, inst.LBIP, inst.Number)
			_, _ = fmt.Fprintf(w, "event: datastar-merge-signals\ndata: %s\n\n", payload)
			_ = w.Flush()

			if isReady {
				break
			}
			time.Sleep(3 * time.Second)
		}
	})

	return nil
}

func ssePatch(c *fiber.Ctx, signals map[string]any, fragments map[string]string) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	var sb strings.Builder
	if len(signals) > 0 {
		var parts []string
		for k, v := range signals {
			switch val := v.(type) {
			case string:
				parts = append(parts, fmt.Sprintf("%s:%q", k, val))
			default:
				parts = append(parts, fmt.Sprintf("%s:%v", k, val))
			}
		}
		sb.WriteString("event: datastar-merge-signals\n")
		sb.WriteString(fmt.Sprintf("data: {%s}\n\n", strings.Join(parts, ",")))
	}

	for selector, html := range fragments {
		sb.WriteString(fmt.Sprintf("event: datastar-merge-fragments\ndata: selector %s\ndata: %s\n\n", selector, html))
	}

	return c.SendString(sb.String())
}
