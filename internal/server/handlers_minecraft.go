package server

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

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
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "GitOps plane disabled — no Codeberg token configured"})
	}
	slug := strings.TrimSpace(c.FormValue("slug"))
	if slug == "" {
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
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-mod-install", strings.Join(slugsToInstall, ", "))
	return c.JSON(fiber.Map{
		"ok":        true,
		"changed":   changed,
		"installed": slugsToInstall,
	})
}

// mcModsRemove removes a mod from neoforge-mods.yaml.
func (s *FiberServer) mcModsRemove(c *fiber.Ctx) error {
	if s.mcMods == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "GitOps plane disabled"})
	}
	slug := strings.TrimSpace(c.FormValue("slug"))
	if slug == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "slug is required"})
	}

	changed, err := s.mcMods.Uninstall(c.UserContext(), slug)
	if err != nil {
		slog.Error("mc-mods remove error", "err", err, "slug", slug)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-mod-remove", slug)
	return c.JSON(fiber.Map{"ok": true, "changed": changed, "slug": slug})
}

// mcVersionSet updates the target Minecraft and NeoForge versions in neoforge-mods.yaml.
func (s *FiberServer) mcVersionSet(c *fiber.Ctx) error {
	if s.mcMods == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "GitOps plane disabled"})
	}
	mcVer := strings.TrimSpace(c.FormValue("minecraft_version"))
	nfVer := strings.TrimSpace(c.FormValue("neoforge_version"))
	if mcVer == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "minecraft_version is required"})
	}

	changed, err := s.mcMods.SetVersion(c.UserContext(), mcVer, nfVer)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-version-set", mcVer+" / "+nfVer)
	return c.JSON(fiber.Map{"ok": true, "changed": changed})
}

// mcAccessGrantOp grants operator status to a player in Git and via live RCON.
func (s *FiberServer) mcAccessGrantOp(c *fiber.Ctx) error {
	if s.mcAccess == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}
	user := strings.TrimSpace(c.FormValue("username"))
	if user == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}

	changed, err := s.mcAccess.GrantOp(c.UserContext(), user)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-op-grant", user)
	return c.JSON(fiber.Map{"ok": true, "changed": changed, "user": user})
}

// mcAccessRevokeOp revokes operator status from a player in Git and via live RCON.
func (s *FiberServer) mcAccessRevokeOp(c *fiber.Ctx) error {
	if s.mcAccess == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}
	user := strings.TrimSpace(c.FormValue("username"))
	if user == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}

	changed, err := s.mcAccess.RevokeOp(c.UserContext(), user)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-op-revoke", user)
	return c.JSON(fiber.Map{"ok": true, "changed": changed, "user": user})
}

// mcAccessAddWhitelist adds a player to the server whitelist in Git and via live RCON.
func (s *FiberServer) mcAccessAddWhitelist(c *fiber.Ctx) error {
	if s.mcAccess == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}
	user := strings.TrimSpace(c.FormValue("username"))
	if user == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}

	changed, err := s.mcAccess.AddWhitelist(c.UserContext(), user)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-whitelist-add", user)
	return c.JSON(fiber.Map{"ok": true, "changed": changed, "user": user})
}

// mcAccessRemoveWhitelist removes a player from the server whitelist in Git and via live RCON.
func (s *FiberServer) mcAccessRemoveWhitelist(c *fiber.Ctx) error {
	if s.mcAccess == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}
	user := strings.TrimSpace(c.FormValue("username"))
	if user == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}

	changed, err := s.mcAccess.RemoveWhitelist(c.UserContext(), user)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-whitelist-remove", user)
	return c.JSON(fiber.Map{"ok": true, "changed": changed, "user": user})
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
