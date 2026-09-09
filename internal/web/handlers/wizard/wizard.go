// Package wizard serves the Minecraft instance provisioning flow: pack/mod
// search, cart validation and creation. Split out of internal/http/instances,
// which it shared no helpers with and only a subset of whose dependencies it
// needs — the narrower Config below is that subset, made explicit.
package wizard

import (
	mccontent "agrelha/internal/app/content"
	"agrelha/internal/domain"
	"bufio"
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/modpack"
	"agrelha/internal/infra/content/mcversions"
	"agrelha/internal/infra/content/modpackindex"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
	"agrelha/internal/web/sse"
)

// MCWizardPage renders the Minecraft creation wizard page.
func (h *Handler) MCWizardPage(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Instance manager not configured")
	}

	instances, err := h.cfg.MCInstances.ListInstances(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("List instances failed: " + err.Error())
	}

	budget := h.cfg.MCInstances.Budget(instances)
	budgetUI := pages.BudgetUI{
		UsedGiB:        budget.UsedGiB,
		TotalBudgetGiB: budget.TotalBudgetGiB,
		RunningCount:   budget.RunningCount,
		MaxRunning:     budget.MaxRunning,
		TotalInstances: budget.TotalInstances,
		MaxInstances:   budget.MaxInstances,
	}

	var releases []string
	if h.cfg.MCV != nil {
		releases = h.cfg.MCV.Releases(c.UserContext(), 15)
	}
	if len(releases) == 0 {
		releases = []string{mcversions.FallbackLatest, "1.21.1", "1.20.1"}
	}

	return shared.Render(c, pages.MinecraftWizard(releases, budgetUI))
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

// MCWizardModpacksSearch searches for modpacks via ModpackIndex.
func (h *Handler) MCWizardModpacksSearch(c *fiber.Ctx) error {
	if h.cfg.MPI == nil {
		return ssePatchElements(c, "#wizard-pack-results", `<div class="col-span-full text-xs text-red-400 py-6 text-center">Modpack Index unconfigured</div>`)
	}

	var req struct {
		Q         string `json:"q" form:"q"`
		PackQuery string `json:"packQuery" form:"packQuery"`
	}
	_ = c.BodyParser(&req)

	q := strings.TrimSpace(req.PackQuery)
	if q == "" {
		q = strings.TrimSpace(req.Q)
	}
	if q == "" {
		q = strings.TrimSpace(c.Query("q"))
	}

	if q == "" {
		return ssePatchElements(c, "#wizard-pack-results", `<div class="col-span-full py-8 text-center text-xs text-zinc-500">Type a modpack name above and click Search to browse available packs.</div>`)
	}

	res, err := h.cfg.MPI.SearchModpacks(c.UserContext(), q, "", 1)
	if err != nil {
		return ssePatchElements(c, "#wizard-pack-results", fmt.Sprintf(`<div class="col-span-full text-xs text-red-400 py-6 text-center">Search error: %s</div>`, html.EscapeString(err.Error())))
	}
	if len(res.Data) == 0 {
		return ssePatchElements(c, "#wizard-pack-results", `<div class="col-span-full text-xs text-zinc-500 py-8 text-center">No modpacks found matching your search.</div>`)
	}

	var sb strings.Builder
	for _, p := range res.Data {
		cleanJSName := strings.ReplaceAll(strings.ReplaceAll(p.Name, `\`, `\\`), `'`, `\'`)
		cleanJSName = strings.ReplaceAll(cleanJSName, `"`, `&quot;`)

		packRefURL := ""
		if p.Links != nil && p.Links["curseforge"] != "" {
			packRefURL = p.Links["curseforge"]
		} else if strings.Contains(p.URL, "curseforge.com") {
			packRefURL = p.URL
		} else if p.URL != "" {
			packRefURL = p.URL
		} else {
			packRefURL = p.PageURL
		}
		cleanRefURL := strings.ReplaceAll(packRefURL, `'`, `\'`)

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
		`, html.EscapeString(p.ThumbnailURL), html.EscapeString(p.Name), p.DownloadCount, html.EscapeString(p.Summary), cleanJSName, p.ID, cleanRefURL))
	}

	return ssePatchElements(c, "#wizard-pack-results", sb.String())
}

// MCWizardModsSearch searches for mods via Modrinth.
func (h *Handler) MCWizardModsSearch(c *fiber.Ctx) error {
	if h.cfg.MR == nil {
		return ssePatchElements(c, "#wizard-mod-results", `<p class="text-xs text-red-400 py-4 text-center">Modrinth client unconfigured</p>`)
	}

	var req struct {
		Q         string `json:"q" form:"q"`
		ModQuery  string `json:"modQuery" form:"modQuery"`
		MCVersion string `json:"mc_version" form:"mc_version"`
		Version   string `json:"version" form:"version"`
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
		return ssePatchElements(c, "#wizard-mod-results", `<p class="text-xs text-zinc-500 py-4 text-center">Search mods to populate results.</p>`)
	}

	mcVersion := strings.TrimSpace(req.MCVersion)
	if mcVersion == "" {
		mcVersion = strings.TrimSpace(req.Version)
	}
	if mcVersion == "" {
		mcVersion = strings.TrimSpace(c.Query("version"))
	}
	if mcVersion == "" {
		mcVersion = "1.21.1"
	}

	res, err := h.cfg.MR.Search(c.UserContext(), q, mcVersion, "", 20, 0)
	if err != nil {
		return ssePatchElements(c, "#wizard-mod-results", fmt.Sprintf(`<p class="text-xs text-red-400 py-4 text-center">Search error: %s</p>`, html.EscapeString(err.Error())))
	}
	if len(res.Hits) == 0 {
		return ssePatchElements(c, "#wizard-mod-results", `<p class="text-xs text-zinc-500 py-4 text-center">No mods found matching your search.</p>`)
	}

	var sb strings.Builder
	for _, hit := range res.Hits {
		cleanSlug := strings.ReplaceAll(hit.Slug, "'", "\\'")
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
					data-on:click="if (!$cart.includes('%s')) { $cart = [...$cart, '%s']; @post('/api/minecraft/wizard/cart/check') }"
					class="shrink-0 rounded bg-zinc-800 px-2.5 py-1 text-xs font-medium text-zinc-300 hover:bg-emerald-700 hover:text-white">
					+ Add
				</button>
			</div>
		`, html.EscapeString(hit.IconURL), html.EscapeString(hit.Title), html.EscapeString(hit.Slug), html.EscapeString(hit.Description), cleanSlug, cleanSlug))
	}

	return ssePatchElements(c, "#wizard-mod-results", sb.String())
}

// MCWizardCartCheck verifies compatibility and loaders for the mods in the cart.
func (h *Handler) MCWizardCartCheck(c *fiber.Ctx) error {
	type cartReq struct {
		Cart      []string `json:"cart"`
		MCVersion string   `json:"mc_version"`
	}
	var req cartReq
	if err := c.BodyParser(&req); err != nil {
		req.Cart = strings.Split(c.FormValue("cart"), ",")
		req.MCVersion = c.FormValue("mc_version")
	}

	compat := mccontent.CheckCartCompatibility(c.UserContext(), h.cfg.MR, req.Cart, req.MCVersion)

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

// MCWizardCreate creates a world according to the wizard parameters.
func (h *Handler) MCWizardCreate(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return shared.SSEToast(c, "err", "Instance manager not configured.", nil)
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
		PackID       string `json:"pack_id" form:"pack_id"`
		RawMods      string `json:"raw_mods" form:"raw_mods"`
		Cart         any    `json:"cart" form:"cart"`
	}
	_ = c.BodyParser(&req)

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.TrimSpace(c.FormValue("name"))
	}
	if name == "" {
		return shared.SSEToast(c, "err", "World name is required.", nil)
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
	tier := domain.NormalizeTier(tierStr)
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

	if h.cfg.MPI != nil && strings.Contains(packRef, "modpackindex.com/modpack/") {
		parts := strings.Split(packRef, "/")
		for i, part := range parts {
			if part == "modpack" && i+1 < len(parts) {
				if id, err := strconv.Atoi(parts[i+1]); err == nil && id > 0 {
					if detail, err := h.cfg.MPI.GetModpack(c.UserContext(), id); err == nil && detail != nil {
						if cfURL := detail.Links["curseforge"]; cfURL != "" {
							packRef = cfURL
						} else if detail.URL != "" && strings.Contains(detail.URL, "curseforge.com") {
							packRef = detail.URL
						}
					}
				}
				break
			}
		}
	}

	if source == "modpack" && h.cfg.MPI != nil {
		packID := strings.TrimSpace(req.PackID)
		if packID == "" {
			packID = strings.TrimSpace(c.FormValue("pack_id"))
		}
		if id, err := strconv.Atoi(packID); err == nil && id > 0 {
			if mods, err := h.cfg.MPI.GetModpackMods(c.UserContext(), id); err == nil && len(mods) > 0 {
				if best := modpackindex.BestLoader(mods); best != "" && best != loader {
					fit := modpackindex.AnalyzeLoader(mods, loader)
					slog.Warn("wizard: loader corrected from pack contents",
						"pack", packName, "requested", loader, "derived", best,
						"would_not_load", len(fit.Blocking))
					loader = best
				}
			}
		}
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

	inst := domain.Instance{
		Name:       name,
		Seed:       seed,
		MCVersion:  mcVersion,
		Tier:       tier,
		MOTD:       motd,
		Difficulty: difficulty,
		Gamemode:   gamemode,
		WorldType:  worldType,
		State:      domain.StateRunning,
	}

	switch source {
	case "vanilla":
		inst.Source = domain.SourceVanilla
		inst.Loader = ""
	case "modpack":
		inst.Source = domain.SourceModpack
		inst.Loader = domain.NormalizeLoader(loader)
		if packName != "" || packRef != "" {
			provider := domain.ProviderCurseForge
			if packProvider == "modrinth" {
				provider = domain.ProviderModrinth
			}
			inst.Pack = &domain.Pack{
				Name:     packName,
				Ref:      packRef,
				Provider: provider,
			}
		}
	default:
		inst.Source = domain.SourceModlist
		inst.Loader = domain.NormalizeLoader(loader)
	}

	created, err := h.cfg.MCInstances.CreateInstance(c.UserContext(), inst, modsTxt)
	if err != nil {
		return shared.SSEToast(c, "err", "Failed to create world: "+err.Error(), nil)
	}

	if h.cfg.Store != nil {
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "mc-instance-create", fmt.Sprintf("World #%02d %q", created.Number, created.Name))
		_ = h.cfg.Store.RecordEvent("mc-instance-create", h.cfg.Actor(c))
	}

	_ = h.cfg.MCInstances.StartInstance(c.UserContext(), created.Number)

	redirectURL := fmt.Sprintf("/minecraft/provisioning/%d", created.Number)
	return shared.SSEToast(c, "ok", fmt.Sprintf("Created world %q! Redirecting...", created.Name), map[string]any{
		"redirect": redirectURL,
	})
}

// MCProvisioningPage renders the progress page while an instance is being initialized.
func (h *Handler) MCProvisioningPage(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Instance manager not configured")
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}

	inst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), num)
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

	return shared.Render(c, pages.MinecraftProvisioning(instUI))
}

// MCProvisioningStream streams pod ready status during instance provisioning.
func (h *Handler) MCProvisioningStream(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil || h.cfg.MCK8s == nil {
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		return c.SendString("event: datastar-patch-signals\ndata: signals {\"phase\":\"ready\",\"ready\":true}\n\n")
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid number")
	}

	inst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Not found")
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		depName := inst.DeploymentName()
		for i := 0; i < 60; i++ {
			desired, ready, err := h.cfg.MCK8s.DeploymentReplicas(context.Background(), depName)
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

			_ = sse.PatchSignals(w, map[string]any{
				"phase": phase,
				"ready": isReady,
				"lb_ip": inst.LBIP,
				"num":   inst.Number,
			})

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

	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	if len(signals) > 0 {
		if err := sse.PatchSignals(w, signals); err != nil {
			return err
		}
	}

	for selector, markup := range fragments {
		if err := sse.InnerElement(w, selector, markup); err != nil {
			return err
		}
	}

	return c.Send(buf.Bytes())
}

// MCWizardImport handles importing a Minecraft modpack archive (.mrpack, .zip, .txt).
func (h *Handler) MCWizardImport(c *fiber.Ctx) error {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "No file uploaded: " + err.Error()})
	}

	f, err := fileHeader.Open()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to open file: " + err.Error()})
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to read file: " + err.Error()})
	}

	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	var world *modpack.ImportedWorld

	if ext == ".mrpack" {
		world, err = modpack.ParseMrpack(bytes.NewReader(data), int64(len(data)))
	} else if ext == ".zip" {
		if w, mrErr := modpack.ParseMrpack(bytes.NewReader(data), int64(len(data))); mrErr == nil {
			world = w
		} else {
			world, err = modpack.ParsePrismZip(bytes.NewReader(data), int64(len(data)))
		}
	} else if ext == ".txt" {
		world = modpack.ParseRawModList(string(data))
	} else {
		if w, zipErr := modpack.ParseMrpack(bytes.NewReader(data), int64(len(data))); zipErr == nil {
			world = w
		} else if w, pErr := modpack.ParsePrismZip(bytes.NewReader(data), int64(len(data))); pErr == nil {
			world = w
		} else {
			world = modpack.ParseRawModList(string(data))
		}
	}

	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Failed to parse archive: " + err.Error()})
	}
	if world == nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Unable to extract world data from archive"})
	}

	if world.Name == "" {
		world.Name = strings.TrimSuffix(fileHeader.Filename, filepath.Ext(fileHeader.Filename))
	}

	return c.JSON(fiber.Map{
		"name":       world.Name,
		"mc_version": world.MCVersion,
		"loader":     world.Loader,
		"raw_mods":   world.RawMods,
		"slugs":      world.Slugs,
	})
}
