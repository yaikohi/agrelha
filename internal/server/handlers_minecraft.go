package server

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/cmd/web/pages"
	"agrelha/internal/minecraft"
	"agrelha/internal/modpack"
)

func isHTMLForm(c *fiber.Ctx) bool {
	accept := c.Get("Accept")
	return strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json")
}

// mcModsPage renders the Minecraft mods, versions, and modpacks page.
func (s *FiberServer) mcModsPage(c *fiber.Ctx) error {
	mcVer := "1.21.1"
	nfVer := "latest"
	var installedMods []string

	if s.mck8s != nil {
		if data, err := s.mck8s.ConfigMapData(c.UserContext(), "minecraft-neoforge-mods"); err == nil {
			if v := strings.TrimSpace(data["MINECRAFT_VERSION"]); v != "" {
				mcVer = v
			}
			if v := strings.TrimSpace(data["NEOFORGE_VERSION"]); v != "" {
				nfVer = v
			}
			installedMods = minecraft.ParseMods(data["mods.txt"])
		}
	}

	tab := c.Query("tab", "modpacks")
	if tab != "modpacks" && tab != "mods" {
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
		mrRes, err := s.mr.Search(c.UserContext(), modQ, mcVer, 24, 0)
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
	return render(c, pages.MinecraftMods(
		mcVer, nfVer,
		installedMods,
		tab,
		modpacksUI,
		packQ,
		filterVer,
		currentPage, totalPages, totalPacks,
		modsUI,
		modQ,
		s.git != nil,
		fk, fm,
	))
}

// mcAccessPage renders the server operators, whitelist, and live players page.
func (s *FiberServer) mcAccessPage(c *fiber.Ctx) error {
	var ops []string
	var whitelist []string

	if s.mck8s != nil {
		if data, err := s.mck8s.ConfigMapData(c.UserContext(), "minecraft-neoforge-access"); err == nil {
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

// mcModsSearch handles searching Modrinth for NeoForge mods.
func (s *FiberServer) mcModsSearch(c *fiber.Ctx) error {
	if s.mr == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Modrinth client unconfigured"})
	}
	q := c.Query("q", "")
	mcVersion := c.Query("version", "1.21.1")
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))

	res, err := s.mr.Search(c.UserContext(), q, mcVersion, limit, offset)
	if err != nil {
		slog.Error("modrinth search error", "err", err, "query", q)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

// mcModsInstall resolves dependencies and commits the requested mod to neoforge-mods.yaml.
func (s *FiberServer) mcModsInstall(c *fiber.Ctx) error {
	if s.mcMods == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "GitOps plane disabled — no Codeberg token configured.")
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "GitOps plane disabled — no Codeberg token configured"})
	}
	slug := strings.TrimSpace(c.FormValue("slug"))
	if slug == "" {
		if isHTMLForm(c) {
			setFlash(c, "err", "Mod slug is required.")
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "slug is required"})
	}
	mcVersion := strings.TrimSpace(c.FormValue("version"))
	if mcVersion == "" {
		mcVersion = "1.21.1"
	}

	ctx := c.UserContext()
	slugsToInstall := []string{slug}

	// Resolve dependencies if Modrinth client available
	if s.mr != nil {
		deps, err := s.mr.ResolveRequiredDependencies(ctx, slug, mcVersion)
		if err != nil {
			slog.Warn("could not resolve all mod dependencies", "slug", slug, "err", err)
		} else {
			slugsToInstall = append(slugsToInstall, deps...)
		}
	}

	changed, err := s.mcMods.Install(ctx, slugsToInstall)
	if err != nil {
		slog.Error("mc-mods install error", "err", err, "slug", slug)
		if isHTMLForm(c) {
			setFlash(c, "err", "Install failed: "+err.Error())
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-mod-install", strings.Join(slugsToInstall, ", "))
	if changed {
		s.applyMinecraftAfterSync("minecraft-neoforge-mods", "mods.txt", func(txt string) bool {
			return strings.Contains(txt, slug)
		})
	}

	if isHTMLForm(c) {
		if changed {
			setFlash(c, "ok", "Installed "+slug+" (and dependencies) — committed to GitOps; server will restart.")
		} else {
			setFlash(c, "ok", slug+" is already installed.")
		}
		return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{
		"ok":        true,
		"changed":   changed,
		"installed": slugsToInstall,
	})
}

// mcModsRemove removes a mod from neoforge-mods.yaml.
func (s *FiberServer) mcModsRemove(c *fiber.Ctx) error {
	if s.mcMods == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "GitOps plane disabled")
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "GitOps plane disabled"})
	}
	slug := strings.TrimSpace(c.FormValue("slug"))
	if slug == "" {
		if isHTMLForm(c) {
			setFlash(c, "err", "Mod slug is required")
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "slug is required"})
	}

	changed, err := s.mcMods.Uninstall(c.UserContext(), slug)
	if err != nil {
		slog.Error("mc-mods remove error", "err", err, "slug", slug)
		if isHTMLForm(c) {
			setFlash(c, "err", "Remove failed: "+err.Error())
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-mod-remove", slug)
	if changed {
		s.applyMinecraftAfterSync("minecraft-neoforge-mods", "mods.txt", func(txt string) bool {
			return !strings.Contains(txt, slug)
		})
	}

	if isHTMLForm(c) {
		if changed {
			setFlash(c, "ok", "Removed "+slug+" — committed to GitOps; server will restart.")
		} else {
			setFlash(c, "ok", slug+" was not installed.")
		}
		return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "changed": changed, "slug": slug})
}

// mcVersionSet updates the target Minecraft and NeoForge versions in neoforge-mods.yaml.
func (s *FiberServer) mcVersionSet(c *fiber.Ctx) error {
	if s.mcMods == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "GitOps plane disabled")
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "GitOps plane disabled"})
	}
	mcVer := strings.TrimSpace(c.FormValue("minecraft_version"))
	nfVer := strings.TrimSpace(c.FormValue("neoforge_version"))
	if mcVer == "" {
		if isHTMLForm(c) {
			setFlash(c, "err", "minecraft_version is required")
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "minecraft_version is required"})
	}
	if nfVer == "" || nfVer == "recommended" {
		nfVer = "latest"
	}

	changed, err := s.mcMods.SetVersion(c.UserContext(), mcVer, nfVer)
	if err != nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Version change failed: "+err.Error())
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-version-set", mcVer+" / "+nfVer)
	if changed {
		s.applyMinecraftAfterSync("minecraft-neoforge-mods", "MINECRAFT_VERSION", func(v string) bool {
			return v == mcVer
		})
	}

	if isHTMLForm(c) {
		if changed {
			setFlash(c, "ok", fmt.Sprintf("Updated versions to Minecraft %s / NeoForge %s — committed; server will restart.", mcVer, nfVer))
		} else {
			setFlash(c, "ok", "Versions already match.")
		}
		return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "changed": changed})
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
	if s.mpi == nil || s.mcMods == nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Modpack or GitOps service unavailable")
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Modpack or GitOps service unavailable"})
	}
	idStr := strings.TrimSpace(c.FormValue("modpack_id"))
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		if isHTMLForm(c) {
			setFlash(c, "err", "Valid modpack_id is required")
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "valid modpack_id is required"})
	}

	ctx := c.UserContext()
	pack, err := s.mpi.GetModpack(ctx, id)
	if err != nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Fetch modpack details: "+err.Error())
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "fetch modpack details: " + err.Error()})
	}

	mods, err := s.mpi.GetModpackMods(ctx, id)
	if err != nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Fetch modpack mods: "+err.Error())
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "fetch modpack mods: " + err.Error()})
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
				if mi.Slug != "" {
					selectedSlug = mi.Slug
					break
				}
			}
		}
		// 2. Fall back to mod slug
		if selectedSlug == "" && m.Slug != "" {
			selectedSlug = m.Slug
		}
		if selectedSlug != "" {
			slugs = append(slugs, selectedSlug)
		}
	}

	changed, err := s.mcMods.SwitchModpack(ctx, pack.Name, targetMCVersion, slugs)
	if err != nil {
		if isHTMLForm(c) {
			setFlash(c, "err", "Switch modpack failed: "+err.Error())
			return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "git commit switch failed: " + err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-modpack-switch", fmt.Sprintf("%s (id=%d, mods=%d)", pack.Name, id, len(slugs)))
	if changed {
		s.applyMinecraftAfterSync("minecraft-neoforge-mods", "mods.txt", func(txt string) bool {
			return strings.Contains(txt, pack.Name)
		})
	}

	if isHTMLForm(c) {
		setFlash(c, "ok", fmt.Sprintf("Switched to modpack %q (%d mods) — committed to GitOps; server will restart.", pack.Name, len(slugs)))
		return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{
		"ok":                true,
		"changed":           changed,
		"modpack_name":      pack.Name,
		"minecraft_version": targetMCVersion,
		"mods_count":        len(slugs),
	})
}

// mcModpackExport generates a Prism Launcher compatible Modrinth modpack (.mrpack)
// for the installed Minecraft NeoForge mods, excluding server-only mods.
func (s *FiberServer) mcModpackExport(c *fiber.Ctx) error {
	if s.mck8s == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "minecraft k8s client not available")
	}
	if s.mr == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "modrinth client not available")
	}
	ctx := c.UserContext()

	data, err := s.mck8s.ConfigMapData(ctx, "minecraft-neoforge-mods")
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "read minecraft-neoforge-mods: "+err.Error())
	}

	mcVer := strings.TrimSpace(data["MINECRAFT_VERSION"])
	if mcVer == "" {
		mcVer = "1.21.1"
	}
	nfVer := strings.TrimSpace(data["NEOFORGE_VERSION"])
	if nfVer == "" {
		nfVer = "recommended"
	}

	slugs := minecraft.ParseMods(data["mods.txt"])
	if len(slugs) == 0 {
		setFlash(c, "err", "No mods are installed — nothing to export.")
		return c.Redirect("/minecraft/mods", fiber.StatusSeeOther)
	}

	configs := map[string]string{}
	if cfgData, err := s.mck8s.ConfigMapData(ctx, mcConfigsCM); err == nil {
		for k, v := range cfgData {
			configs[k] = v
		}
	}

	packName := "Minecraft NeoForge"
	blob, err := modpack.BuildMrpack(ctx, s.mr, packName, mcVer, nfVer, slugs, configs)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "build mrpack: "+err.Error())
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-modpack-export", fmt.Sprintf("mc=%s, nf=%s, mods=%d", mcVer, nfVer, len(slugs)))

	filename := fmt.Sprintf("minecraft-neoforge-client-%s.mrpack", time.Now().Format("2006-01-02"))
	c.Set("Content-Type", "application/x-modrinth-modpack+zip")
	c.Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	return c.Send(blob)
}
