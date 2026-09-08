package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/cmd/web/pages"
	"agrelha/internal/mcversions"
	"agrelha/internal/minecraft"
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
