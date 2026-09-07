package server

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/cmd/web/pages"
	"agrelha/internal/mcversions"
	"agrelha/internal/minecraft"
	"agrelha/internal/modpack"
	"agrelha/internal/modpackindex"
	"agrelha/internal/store"
)

func (s *FiberServer) defaultMCVersion(ctx context.Context) string {
	if s.mcv == nil {
		return mcversions.FallbackLatest
	}
	return s.mcv.Latest(ctx)
}

func (s *FiberServer) mcVersionChoices(ctx context.Context, current string) []string {
	var out []string
	if s.mcv != nil {
		out = s.mcv.Releases(ctx, 15)
	}
	if len(out) == 0 {
		out = []string{mcversions.FallbackLatest}
	}
	if current == "" {
		return out
	}
	for _, v := range out {
		if v == current {
			return out
		}
	}
	return append([]string{current}, out...)
}

func loaderMatches(loaders []string, target string) bool {
	if len(loaders) == 0 {
		return true
	}
	for _, l := range loaders {
		l = strings.ToLower(strings.TrimSpace(l))
		if l == target || (target == "neoforge" && l == "forge") {
			return true
		}
	}
	return false
}

func isHTMLForm(c *fiber.Ctx) bool {
	accept := c.Get("Accept")
	return strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json")
}

const slotConfigMap = "minecraft-modded-slot"

func (s *FiberServer) mcLoaderManager(loader string) (*minecraft.ModManager, string, string) {
	return s.mcMods, "minecraft-modded-mods", s.cfg.MinecraftDeployment
}

// activeSlot reconstructs the domain model of the running slot from the live
// ConfigMap: intent from annotations, falling back to derived env where needed.
func (s *FiberServer) activeSlot(ctx context.Context) minecraft.Slot {
	if s.mck8s == nil {
		return minecraft.Slot{Loader: minecraft.LoaderNeoForge, Source: minecraft.SourceModlist}
	}
	data, ann, err := s.mck8s.ConfigMapMeta(ctx, slotConfigMap)
	if err != nil {
		return minecraft.Slot{Loader: minecraft.LoaderNeoForge, Source: minecraft.SourceModlist}
	}
	return minecraft.SlotFromAnnotations(ann, data)
}

// mcModsPage renders the Minecraft mods, versions, and modpacks page.
func (s *FiberServer) mcModsPage(c *fiber.Ctx) error {
	active := s.activeSlot(c.UserContext())
	activeRunningLoader := string(minecraft.NormalizeLoader(string(active.Loader)))

	currentLoader := strings.ToLower(strings.TrimSpace(c.Query("loader")))
	if currentLoader != "fabric" && currentLoader != "neoforge" {
		currentLoader = activeRunningLoader
	}

	cmName := "minecraft-modded-mods"
	versionKey := "NEOFORGE_VERSION"
	if currentLoader == "fabric" {
		versionKey = "FABRIC_VERSION"
	}

	mcVer := s.defaultMCVersion(c.UserContext())
	loaderVer := "latest"
	var installedMods []string

	if s.mck8s != nil {
		if data, err := s.mck8s.ConfigMapData(c.UserContext(), cmName); err == nil {
			if v := strings.TrimSpace(data["MINECRAFT_VERSION"]); v != "" {
				mcVer = v
			}
			if v := strings.TrimSpace(data[versionKey]); v != "" {
				loaderVer = v
			}
			installedMods = minecraft.ParseMods(data["mods.txt"])
		}
	}

	tab := c.Query("tab", "modpacks")
	if tab != "modpacks" && tab != "mods" && tab != "worlds" {
		tab = "modpacks"
	}

	packQ := strings.TrimSpace(c.Query("q"))
	if packQ == "" {
		packQ = strings.TrimSpace(c.Query("pack_query"))
	}
	filterVer := strings.TrimSpace(c.Query("filter_version"))
	page, _ := strconv.Atoi(c.Query("page", "1"))
	if page < 1 {
		page = 1
	}

	var modpacksUI []pages.ModpackUI
	currentPage := 1
	totalPages := 1
	totalPacks := 0

	if s.mpi != nil {
		res, err := s.mpi.SearchModpacks(c.UserContext(), packQ, filterVer, page)
		if err == nil && res != nil {
			for _, p := range res.Data {
				modpacksUI = append(modpacksUI, pages.ModpackUI{
					ID:            p.ID,
					Name:          p.Name,
					Summary:       p.Summary,
					ThumbnailURL:  p.ThumbnailURL,
					DownloadCount: p.DownloadCount,
					PageURL:       p.URL,
				})
			}
			if res.Meta.CurrentPage > 0 {
				currentPage = res.Meta.CurrentPage
			}
			if res.Meta.LastPage > 0 {
				totalPages = res.Meta.LastPage
			}
			totalPacks = res.Meta.Total
		}
	}

	modQ := strings.TrimSpace(c.Query("mod_q"))
	var modsUI []pages.ModUI
	if tab == "mods" && s.mr != nil {
		installedSet := make(map[string]bool, len(installedMods))
		for _, m := range installedMods {
			installedSet[m] = true
		}
		mrRes, err := s.mr.Search(c.UserContext(), modQ, mcVer, currentLoader, 24, 0)
		if err == nil && mrRes != nil {
			for _, hit := range mrRes.Hits {
				modsUI = append(modsUI, pages.ModUI{
					Slug:        hit.Slug,
					Title:       hit.Title,
					Description: hit.Description,
					IconURL:     hit.IconURL,
					Downloads:   hit.Downloads,
					Author:      hit.Author,
					Installed:   installedSet[hit.Slug],
				})
			}
		}
	}

	fk, fm := takeFlash(c)
	curSlot, slotList := s.knownSlots(c.UserContext())

	return render(c, pages.MinecraftMods(
		mcVer, loaderVer,
		s.mcVersionChoices(c.UserContext(), mcVer),
		curSlot,
		slotList,
		installedMods,
		tab,
		modpacksUI,
		packQ,
		filterVer,
		currentPage, totalPages, totalPacks,
		modsUI,
		modQ,
		currentLoader, activeRunningLoader,
		s.git != nil,
		fk, fm,
	))
}

// mcAccessPage renders the server operators, whitelist, and live players page.
func (s *FiberServer) mcAccessPage(c *fiber.Ctx) error {
	var ops []string
	var whitelist []string

	if s.mck8s != nil {
		data, err := s.mck8s.ConfigMapData(c.UserContext(), "minecraft-modded-access")
		if err != nil {
			data, err = s.mck8s.ConfigMapData(c.UserContext(), "minecraft-neoforge-access")
		}
		if err == nil {
			ops = minecraft.ParseUsers(data["ops.txt"])
			whitelist = minecraft.ParseUsers(data["whitelist.txt"])
		}
	}

	var onlinePlayers []string
	whitelistEnforced := false
	if s.mcAccess != nil {
		if pl, err := s.mcAccess.OnlinePlayers(); err == nil {
			onlinePlayers = pl
		}
		if enf, err := s.mcAccess.WhitelistEnforced(); err == nil {
			whitelistEnforced = enf
		}
	}

	fk, fm := takeFlash(c)
	return render(c, pages.MinecraftAccess(ops, whitelist, onlinePlayers, whitelistEnforced, s.git != nil, fk, fm))
}

// mcModsSearch handles searching Modrinth for mods.
func (s *FiberServer) mcModsSearch(c *fiber.Ctx) error {
	if s.mr == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Modrinth client unconfigured"})
	}
	q := c.Query("q", "")
	mcVersion := strings.TrimSpace(c.Query("version"))
	if mcVersion == "" {
		mcVersion = s.defaultMCVersion(c.UserContext())
	}
	loader := c.Query("loader", "neoforge")
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))

	res, err := s.mr.Search(c.UserContext(), q, mcVersion, loader, limit, offset)
	if err != nil {
		slog.Error("modrinth search error", "err", err, "query", q, "loader", loader)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

// mcModsInstall resolves dependencies and commits the requested mod to the loader's mods.yaml.
func (s *FiberServer) mcModsInstall(c *fiber.Ctx) error {
	loader := strings.ToLower(strings.TrimSpace(c.FormValue("loader")))
	if loader != "fabric" {
		loader = "neoforge"
	}
	mgr, cmName, depName := s.mcLoaderManager(loader)
	redirectURL := fmt.Sprintf("/minecraft/mods?loader=%s&tab=mods", loader)

	if mgr == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "GitOps plane disabled — no Codeberg token configured.")
			return c.Redirect(redirectURL, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "GitOps plane disabled — no Codeberg token configured"})
	}
	slug := strings.TrimSpace(c.FormValue("slug"))
	if slug == "" {
		if isHTMLForm(c) {
			setFlash(c, "err", "Mod slug is required.")
			return c.Redirect(redirectURL, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "slug is required"})
	}
	mcVersion := strings.TrimSpace(c.FormValue("version"))
	if mcVersion == "" {
		mcVersion = s.defaultMCVersion(c.UserContext())
	}

	ctx := c.UserContext()
	slugsToInstall := []string{slug}

	// Resolve dependencies if Modrinth client available
	if s.mr != nil {
		deps, err := s.mr.ResolveRequiredDependencies(ctx, slug, mcVersion, loader)
		if err != nil {
			slog.Warn("could not resolve all mod dependencies", "slug", slug, "loader", loader, "err", err)
		} else {
			slugsToInstall = append(slugsToInstall, deps...)
		}
	}

	changed, err := mgr.Install(ctx, slugsToInstall)
	if err != nil {
		slog.Error("mc-mods install error", "loader", loader, "err", err, "slug", slug)
		if isHTMLForm(c) {
			setFlash(c, "err", "Install failed: "+err.Error())
			return c.Redirect(redirectURL, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-mod-install", fmt.Sprintf("[%s] %s", loader, strings.Join(slugsToInstall, ", ")))
	if changed {
		s.applyMinecraftAfterSync(cmName, depName, "mods.txt", func(txt string) bool {
			return strings.Contains(txt, slug)
		})
	}

	if isHTMLForm(c) {
		if changed {
			setFlash(c, "ok", fmt.Sprintf("Installed %s (and dependencies) for %s — committed to GitOps; server will restart.", slug, strings.Title(loader)))
		} else {
			setFlash(c, "ok", slug+" is already installed.")
		}
		return c.Redirect(redirectURL, fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{
		"ok":        true,
		"loader":    loader,
		"changed":   changed,
		"installed": slugsToInstall,
	})
}

// mcModsRemove removes a mod from the loader's mods.yaml.
func (s *FiberServer) mcModsRemove(c *fiber.Ctx) error {
	loader := strings.ToLower(strings.TrimSpace(c.FormValue("loader")))
	if loader != "fabric" {
		loader = "neoforge"
	}
	mgr, cmName, depName := s.mcLoaderManager(loader)
	redirectURL := fmt.Sprintf("/minecraft/mods?loader=%s", loader)

	if mgr == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "GitOps plane disabled")
			return c.Redirect(redirectURL, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "GitOps plane disabled"})
	}
	slug := strings.TrimSpace(c.FormValue("slug"))
	if slug == "" {
		if isHTMLForm(c) {
			setFlash(c, "err", "Mod slug is required")
			return c.Redirect(redirectURL, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "slug is required"})
	}

	changed, err := mgr.Uninstall(c.UserContext(), slug)
	if err != nil {
		slog.Error("mc-mods remove error", "loader", loader, "err", err, "slug", slug)
		if isHTMLForm(c) {
			setFlash(c, "err", "Remove failed: "+err.Error())
			return c.Redirect(redirectURL, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-mod-remove", fmt.Sprintf("[%s] %s", loader, slug))
	if changed {
		s.applyMinecraftAfterSync(cmName, depName, "mods.txt", func(txt string) bool {
			return !strings.Contains(txt, slug)
		})
	}

	if isHTMLForm(c) {
		if changed {
			setFlash(c, "ok", fmt.Sprintf("Removed %s from %s — committed to GitOps; server will restart.", slug, strings.Title(loader)))
		} else {
			setFlash(c, "ok", slug+" was not installed.")
		}
		return c.Redirect(redirectURL, fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "loader": loader, "changed": changed, "slug": slug})
}

// mcVersionSet updates the target Minecraft and loader versions.
func (s *FiberServer) mcVersionSet(c *fiber.Ctx) error {
	loader := strings.ToLower(strings.TrimSpace(c.FormValue("loader")))
	if loader != "fabric" {
		loader = "neoforge"
	}
	mgr, cmName, depName := s.mcLoaderManager(loader)
	redirectURL := fmt.Sprintf("/minecraft/mods?loader=%s", loader)

	if mgr == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "GitOps plane disabled")
			return c.Redirect(redirectURL, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "GitOps plane disabled"})
	}
	mcVer := strings.TrimSpace(c.FormValue("minecraft_version"))
	loaderVer := strings.TrimSpace(c.FormValue("loader_version"))
	if loaderVer == "" {
		loaderVer = strings.TrimSpace(c.FormValue("neoforge_version"))
	}
	if loaderVer == "" {
		loaderVer = strings.TrimSpace(c.FormValue("fabric_version"))
	}
	if mcVer == "" {
		if isHTMLForm(c) {
			setFlash(c, "err", "minecraft_version is required")
			return c.Redirect(redirectURL, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "minecraft_version is required"})
	}
	if loaderVer == "" || loaderVer == "recommended" {
		loaderVer = "latest"
	}

	changed, err := mgr.SetVersion(c.UserContext(), mcVer, loaderVer)
	if err != nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Version change failed: "+err.Error())
			return c.Redirect(redirectURL, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-version-set", fmt.Sprintf("[%s] %s / %s", loader, mcVer, loaderVer))
	if changed {
		s.applyMinecraftAfterSync(cmName, depName, "MINECRAFT_VERSION", func(v string) bool {
			return v == mcVer
		})
	}

	if isHTMLForm(c) {
		if changed {
			setFlash(c, "ok", fmt.Sprintf("Updated %s versions to Minecraft %s / %s — committed; server will restart.", strings.Title(loader), mcVer, loaderVer))
		} else {
			setFlash(c, "ok", "Versions already match.")
		}
		return c.Redirect(redirectURL, fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "loader": loader, "changed": changed})
}

// mcAccessGrantOp grants operator status to a player in Git and via live RCON.
func (s *FiberServer) mcAccessGrantOp(c *fiber.Ctx) error {
	if s.mcAccess == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Access manager unconfigured")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}
	user := strings.TrimSpace(c.FormValue("username"))
	if user == "" {
		if isHTMLForm(c) {
			setFlash(c, "err", "username is required")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}

	changed, err := s.mcAccess.GrantOp(c.UserContext(), user)
	if err != nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Grant op failed: "+err.Error())
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-op-grant", user)

	if isHTMLForm(c) {
		if changed {
			setFlash(c, "ok", "Granted operator status to "+user+" — live applied via RCON and committed to Git.")
		} else {
			setFlash(c, "ok", user+" is already an operator.")
		}
		return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "changed": changed, "user": user})
}

// mcAccessRevokeOp revokes operator status from a player in Git and via live RCON.
func (s *FiberServer) mcAccessRevokeOp(c *fiber.Ctx) error {
	if s.mcAccess == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Access manager unconfigured")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}
	user := strings.TrimSpace(c.FormValue("username"))
	if user == "" {
		if isHTMLForm(c) {
			setFlash(c, "err", "username is required")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}

	changed, err := s.mcAccess.RevokeOp(c.UserContext(), user)
	if err != nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Revoke op failed: "+err.Error())
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-op-revoke", user)

	if isHTMLForm(c) {
		if changed {
			setFlash(c, "ok", "Revoked operator status from "+user+" — live applied via RCON and committed to Git.")
		} else {
			setFlash(c, "ok", user+" is not an operator.")
		}
		return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "changed": changed, "user": user})
}

// mcAccessAddWhitelist adds a player to the server whitelist in Git and via live RCON.
func (s *FiberServer) mcAccessAddWhitelist(c *fiber.Ctx) error {
	if s.mcAccess == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Access manager unconfigured")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}
	user := strings.TrimSpace(c.FormValue("username"))
	if user == "" {
		if isHTMLForm(c) {
			setFlash(c, "err", "username is required")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}

	changed, err := s.mcAccess.AddWhitelist(c.UserContext(), user)
	if err != nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Add whitelist failed: "+err.Error())
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-whitelist-add", user)

	if isHTMLForm(c) {
		if changed {
			setFlash(c, "ok", "Added "+user+" to whitelist — live applied via RCON and committed to Git.")
		} else {
			setFlash(c, "ok", user+" is already whitelisted.")
		}
		return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "changed": changed, "user": user})
}

// mcAccessRemoveWhitelist removes a player from the server whitelist in Git and via live RCON.
func (s *FiberServer) mcAccessRemoveWhitelist(c *fiber.Ctx) error {
	if s.mcAccess == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Access manager unconfigured")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}
	user := strings.TrimSpace(c.FormValue("username"))
	if user == "" {
		if isHTMLForm(c) {
			setFlash(c, "err", "username is required")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}

	changed, err := s.mcAccess.RemoveWhitelist(c.UserContext(), user)
	if err != nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Remove whitelist failed: "+err.Error())
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-whitelist-remove", user)

	if isHTMLForm(c) {
		if changed {
			setFlash(c, "ok", "Removed "+user+" from whitelist — live applied via RCON and committed to Git.")
		} else {
			setFlash(c, "ok", user+" was not in whitelist.")
		}
		return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "changed": changed, "user": user})
}

// mcAccessWhitelistToggle toggles in-game whitelist enforcement via RCON.
func (s *FiberServer) mcAccessWhitelistToggle(c *fiber.Ctx) error {
	if s.mcAccess == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Access manager unconfigured")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}

	current, err := s.mcAccess.WhitelistEnforced()
	if err != nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Failed to query whitelist status: "+err.Error())
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	target := !current
	if err := s.mcAccess.SetWhitelistEnforced(target); err != nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Failed to toggle whitelist: "+err.Error())
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	actionDesc := "enabled"
	if !target {
		actionDesc = "disabled"
	}
	_ = s.store.RecordAudit(s.actor(c), "mc-whitelist-toggle", actionDesc)

	if isHTMLForm(c) {
		setFlash(c, "ok", fmt.Sprintf("Whitelist enforcement %s via live RCON.", actionDesc))
		return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
	}
	return c.JSON(fiber.Map{"ok": true, "enforced": target})
}

// mcOnlinePlayers queries live online players via RCON.
func (s *FiberServer) mcOnlinePlayers(c *fiber.Ctx) error {
	if s.mcAccess == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}
	players, err := s.mcAccess.OnlinePlayers()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true, "players": players})
}

// mcModpacksSearch searches Modpack Index for modpacks.
func (s *FiberServer) mcModpacksSearch(c *fiber.Ctx) error {
	if s.mpi == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Modpack Index client unconfigured"})
	}
	q := c.Query("q", "")
	mcVersion := c.Query("version", "")
	page, _ := strconv.Atoi(c.Query("page", "1"))

	res, err := s.mpi.SearchModpacks(c.UserContext(), q, mcVersion, page)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

// mcModpackGet returns details and mods for a specific modpack from Modpack Index.
func (s *FiberServer) mcModpackGet(c *fiber.Ctx) error {
	if s.mpi == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Modpack Index client unconfigured"})
	}
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil || id <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "valid id required"})
	}

	ctx := c.UserContext()
	detail, err := s.mpi.GetModpack(ctx, id)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	mods, err := s.mpi.GetModpackMods(ctx, id)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"modpack": detail,
		"mods":    mods,
	})
}

// mcModpackSwitch switches the server's modpack by pulling the mod list from Modpack Index.
func (s *FiberServer) mcModpackSwitch(c *fiber.Ctx) error {
	loader := strings.ToLower(strings.TrimSpace(c.FormValue("loader")))
	if loader != "fabric" {
		loader = "neoforge"
	}
	mgr, cmName, depName := s.mcLoaderManager(loader)
	redirectURL := fmt.Sprintf("/minecraft/mods?loader=%s", loader)

	if s.mpi == nil || mgr == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Modpack or GitOps service unavailable")
			return c.Redirect(redirectURL, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Modpack or GitOps service unavailable"})
	}
	idStr := strings.TrimSpace(c.FormValue("modpack_id"))
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		if isHTMLForm(c) {
			setFlash(c, "err", "Valid modpack_id is required")
			return c.Redirect(redirectURL, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "valid modpack_id is required"})
	}

	ctx := c.UserContext()
	pack, err := s.mpi.GetModpack(ctx, id)
	if err != nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Fetch modpack details: "+err.Error())
			return c.Redirect(redirectURL, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "fetch modpack details: " + err.Error()})
	}

	mods, err := s.mpi.GetModpackMods(ctx, id)
	if err != nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Fetch modpack mods: "+err.Error())
			return c.Redirect(redirectURL, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "fetch modpack mods: " + err.Error()})
	}

	// Loader gate. modpackindex's public API exposes no loader field (verified:
	// no `loaders` on search/detail, loader filter params are ignored, /loaders
	// 404s, and the site's own panel is fed by a private v2 API that 403s), so
	// the pack's loader is derived from which loaders its mods can actually run
	// on. A raw per-loader tally is misleading because most mods are
	// multi-loader — what matters is how many mods the target CANNOT run.
	if ok, best, targetFit, bestFit := modpackindex.CheckLoader(mods, loader); !ok {
		if strings.EqualFold(strings.TrimSpace(c.FormValue("force")), "true") {
			slog.Warn("modpack loader mismatch overridden",
				"pack", pack.Name, "target", loader, "best", best, "blocking", len(targetFit.Blocking))
		} else {
			msg := targetFit.Explain(pack.Name, best, bestFit)
			slog.Warn("modpack loader mismatch refused", "pack", pack.Name, "target", loader, "best", best)
			if isHTMLForm(c) {
				setFlash(c, "err", msg)
				return c.Redirect(redirectURL, fiber.StatusSeeOther)
			}
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error":          msg,
				"pack":           pack.Name,
				"target_loader":  loader,
				"pack_loader":    best,
				"blocking_mods":  targetFit.Blocking,
				"target_support": fmt.Sprintf("%d/%d", targetFit.Supported, targetFit.Known),
				"best_support":   fmt.Sprintf("%d/%d", bestFit.Supported, bestFit.Known),
			})
		}
	}

	var targetMCVersion string
	if len(pack.MinecraftVersions) > 0 {
		targetMCVersion = pack.MinecraftVersions[0].Name
	}

	var slugs []string
	for _, m := range mods {
		var selectedSlug string
		// 1. Prefer Modrinth slug if available
		if len(m.ModrinthInfo) > 0 {
			for _, mi := range m.ModrinthInfo {
				if mi.Slug != "" && loaderMatches(mi.Loaders, loader) {
					selectedSlug = mi.Slug
					break
				}
			}
		}
		// 2. Fall back to mod slug (marked optional with '?' so mc-image-helper won't crash if CurseForge-only)
		if selectedSlug == "" && m.Slug != "" {
			selectedSlug = m.Slug + "?"
		}
		if selectedSlug != "" {
			slugs = append(slugs, selectedSlug)
		}
	}

	if s.mcSlot == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "GitOps plane disabled — no Codeberg token configured.")
			return c.Redirect(redirectURL, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "GitOps plane disabled"})
	}
	ld := minecraft.NormalizeLoader(loader)
	changed, err := s.mcSlot.SwitchToModList(ctx, pack.Name, ld, targetMCVersion, slugs)
	s.registerSlot(minecraft.Slot{
		Name:      pack.Name,
		Loader:    ld,
		Source:    minecraft.SourceModlist,
		MCVersion: targetMCVersion,
	})
	if err != nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Switch modpack failed: "+err.Error())
			return c.Redirect(redirectURL, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "git commit switch failed: " + err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-modpack-switch", fmt.Sprintf("[%s] %s (id=%d, mods=%d)", loader, pack.Name, id, len(slugs)))
	if changed {
		s.applyMinecraftAfterSync(cmName, depName, "mods.txt", func(txt string) bool {
			return strings.Contains(txt, pack.Name)
		})
	}

	if isHTMLForm(c) {
		setFlash(c, "ok", fmt.Sprintf("Switched %s to modpack %q (%d mods) — committed to GitOps; server will restart.", strings.Title(loader), pack.Name, len(slugs)))
		return c.Redirect(redirectURL, fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "pack": pack.Name, "mods": len(slugs), "changed": changed})
}

// mcModpackExport generates a Prism Launcher compatible Modrinth modpack (.mrpack)
// for the installed mods, excluding server-only mods.
func (s *FiberServer) mcModpackExport(c *fiber.Ctx) error {
	if s.mck8s == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "minecraft k8s client not available")
	}
	if s.mr == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "modrinth client not available")
	}
	ctx := c.UserContext()

	loader := strings.ToLower(strings.TrimSpace(c.Query("loader")))
	if loader != "fabric" {
		loader = "neoforge"
	}
	cmName := "minecraft-modded-mods"
	versionKey := "NEOFORGE_VERSION"
	packName := "Minecraft NeoForge"
	cfgCM := "minecraft-neoforge-configs"
	filename := fmt.Sprintf("minecraft-neoforge-client-%s.mrpack", time.Now().Format("2006-01-02"))

	if loader == "fabric" {
		versionKey = "FABRIC_VERSION"
		packName = "Minecraft Fabric"
		cfgCM = "minecraft-fabric-configs"
		filename = fmt.Sprintf("minecraft-fabric-client-%s.mrpack", time.Now().Format("2006-01-02"))
	}

	redirectURL := fmt.Sprintf("/minecraft/mods?loader=%s", loader)
	data, err := s.mck8s.ConfigMapData(ctx, cmName)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "read "+cmName+": "+err.Error())
	}

	mcVer := strings.TrimSpace(data["MINECRAFT_VERSION"])
	if mcVer == "" {
		mcVer = s.defaultMCVersion(c.UserContext())
	}
	loaderVer := strings.TrimSpace(data[versionKey])
	if loaderVer == "" {
		loaderVer = "latest"
	}

	// Try extracting friendly pack name from header comment (e.g. "# Modpack: Neuroshroud: The Abandoned")
	for _, line := range strings.Split(data["mods.txt"], "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# Modpack:") {
			candidate := strings.TrimSpace(strings.TrimPrefix(line, "# Modpack:"))
			if candidate != "" {
				packName = candidate
			}
			break
		}
	}

	rawSlugs := minecraft.ParseMods(data["mods.txt"])
	if len(rawSlugs) == 0 {
		setFlash(c, "err", "No mods are installed — nothing to export.")
		return c.Redirect(redirectURL, fiber.StatusSeeOther)
	}

	var slugs []string
	for _, s := range rawSlugs {
		cleaned := strings.TrimSuffix(strings.TrimSpace(s), "?")
		if cleaned != "" {
			slugs = append(slugs, cleaned)
		}
	}

	configs := map[string]string{}
	if cfgData, err := s.mck8s.ConfigMapData(ctx, cfgCM); err == nil {
		for k, v := range cfgData {
			configs[k] = v
		}
	}

	blob, err := modpack.BuildMrpack(ctx, s.mr, packName, mcVer, loader, loaderVer, slugs, configs)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "build mrpack: "+err.Error())
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-modpack-export", fmt.Sprintf("[%s] name=%q, mc=%s, ver=%s, mods=%d", loader, packName, mcVer, loaderVer, len(slugs)))

	c.Set("Content-Type", "application/x-modrinth-modpack+zip")
	c.Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	return c.Send(blob)
}

// mcLoaderSwitch switches the live Minecraft engine between NeoForge and Fabric.
func (s *FiberServer) mcLoaderSwitch(c *fiber.Ctx) error {
	target := strings.ToLower(strings.TrimSpace(c.FormValue("target")))
	if target != "fabric" && target != "neoforge" {
		if isHTMLForm(c) {
			setFlash(c, "err", "Invalid target loader (must be fabric or neoforge)")
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "target must be fabric or neoforge"})
	}

	if s.mck8s == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Minecraft cluster client unconfigured")
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "minecraft cluster client unconfigured"})
	}

	ctx := c.UserContext()
	if s.mcSlot == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "GitOps plane disabled — no Codeberg token configured.")
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "GitOps plane disabled"})
	}

	cur := s.activeSlot(ctx)
	if cur.Name == "" {
		cur.Name = target
	}
	if cur.MCVersion == "" {
		cur.MCVersion = s.defaultMCVersion(ctx)
	}

	// SetLoader refuses when the slot is pack-defined: the pack decides the
	// loader, and silently rewriting it is what destroyed a slot before.
	if _, err := s.mcSlot.SetLoader(ctx, cur, minecraft.NormalizeLoader(target)); err != nil {
		slog.Warn("mc loader switch refused", "target", target, "slot", cur.Name, "err", err)
		if isHTMLForm(c) {
			setFlash(c, "err", err.Error())
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
	}
	s.registerSlot(cur)

	_ = s.store.RecordAudit(s.actor(c), "mc-loader-switch", target)
	_ = s.store.RecordEvent("mc-loader-switch", target)
	s.applyMinecraftAfterSync(slotConfigMap, s.cfg.MinecraftDeployment, "TYPE", func(v string) bool {
		return strings.EqualFold(v, target)
	})

	if isHTMLForm(c) {
		displayName := "NeoForge"
		if target == "fabric" {
			displayName = "Fabric"
		}
		setFlash(c, "ok", fmt.Sprintf("Switched active engine to %s — pod is starting.", displayName))
		return c.Redirect("/minecraft/mods?loader="+target, fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "loader": target})
}

func (s *FiberServer) registerSlot(sl minecraft.Slot) {
	if s.store == nil {
		return
	}
	row := store.Slot{
		Slot:      minecraft.SlotName(sl.Name),
		Loader:    string(minecraft.NormalizeLoader(string(sl.Loader))),
		Source:    string(minecraft.NormalizeSource(string(sl.Source))),
		MCVersion: sl.MCVersion,
	}
	if sl.PackDefined() {
		row.PackProvider = string(sl.Pack.Provider)
		row.PackRef = sl.Pack.Ref
		row.Pack = sl.Pack.Name
	}
	_ = s.store.UpsertSlot(row)
}

func (s *FiberServer) knownSlots(ctx context.Context) (current string, slots []pages.SlotUI) {
	active := s.activeSlot(ctx)
	current = minecraft.SlotName(active.Name)
	if active.Name == "" {
		current = ""
	}

	if s.store != nil {
		if rows, err := s.store.ListSlots(); err == nil {
			for _, r := range rows {
				slots = append(slots, pages.SlotUI{
					Slot:         r.Slot,
					Loader:       r.Loader,
					Source:       r.Source,
					Pack:         r.Pack,
					PackProvider: r.PackProvider,
					MCVersion:    r.MCVersion,
					Active:       r.Slot == current,
					LastUsed:     humanAgo(r.LastUsed),
				})
			}
		}
	}

	if current != "" {
		found := false
		for _, sl := range slots {
			if sl.Active {
				found = true
				break
			}
		}
		if !found {
			ui := pages.SlotUI{
				Slot:      current,
				Loader:    string(minecraft.NormalizeLoader(string(active.Loader))),
				Source:    string(minecraft.NormalizeSource(string(active.Source))),
				MCVersion: active.MCVersion,
				Active:    true,
			}
			if active.PackDefined() {
				ui.Pack = active.Pack.Name
				ui.PackProvider = string(active.Pack.Provider)
			}
			slots = append([]pages.SlotUI{ui}, slots...)
		}
	}
	return current, slots
}

// mcSlotSwitch reactivates a previously used world slot without changing its pack.
func (s *FiberServer) mcSlotSwitch(c *fiber.Ctx) error {
	target := minecraft.SlotName(c.FormValue("slot"))
	redirect := "/minecraft/mods?tab=worlds"

	if target == "" || target == "default" {
		if isHTMLForm(c) {
			setFlash(c, "err", "No world slot given.")
			return c.Redirect(redirect, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "slot is required"})
	}
	if s.mcSlot == nil || s.store == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "GitOps plane disabled — no Codeberg token configured.")
			return c.Redirect(redirect, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "GitOps plane disabled"})
	}

	rows, err := s.store.ListSlots()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	var want *store.Slot
	for i := range rows {
		if rows[i].Slot == target {
			want = &rows[i]
			break
		}
	}
	if want == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Unknown world slot: "+target)
			return c.Redirect(redirect, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "unknown slot"})
	}

	sl := minecraft.Slot{
		Name:      want.Slot,
		Loader:    minecraft.NormalizeLoader(want.Loader),
		Source:    minecraft.NormalizeSource(want.Source),
		MCVersion: want.MCVersion,
	}
	if sl.Source == minecraft.SourceModpack && want.PackRef != "" {
		provider := minecraft.Provider(want.PackProvider)
		if provider == "" {
			provider = minecraft.ProviderCurseForge
		}
		sl.Pack = &minecraft.Pack{Provider: provider, Ref: want.PackRef, Name: want.Pack}
	}
	if want.Pack != "" {
		sl.MOTD = fmt.Sprintf("%s (nf.ykhi.xyz)", want.Pack)
	}

	ctx := c.UserContext()
	msg := fmt.Sprintf("mc-slot: reactivate %s", want.Slot)
	if _, err := s.mcSlot.Apply(ctx, sl, msg); err != nil {
		slog.Error("mc slot switch failed", "slot", target, "err", err)
		if isHTMLForm(c) {
			setFlash(c, "err", "Failed to switch world: "+err.Error())
			return c.Redirect(redirect, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	s.registerSlot(sl)
	_ = s.store.RecordAudit(s.actor(c), "mc-slot-switch", target)
	s.applyMinecraftAfterSync(slotConfigMap, s.cfg.MinecraftDeployment, "WORLD_SLOT", func(v string) bool {
		return v == target
	})

	if isHTMLForm(c) {
		setFlash(c, "ok", fmt.Sprintf("Switched to world %q — committed to GitOps; server will restart.", target))
		return c.Redirect(redirect, fiber.StatusSeeOther)
	}
	return c.JSON(fiber.Map{"ok": true, "slot": target})
}
