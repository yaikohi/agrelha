package access

import (
	"fmt"
	"strings"

	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"

	"github.com/gofiber/fiber/v2"
)

func isHTMLForm(c *fiber.Ctx) bool {
	accept := c.Get("Accept")
	return strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json")
}

// MCAccessPage renders the server operators, whitelist, and live players page.
func (h *Handler) MCAccessPage(c *fiber.Ctx) error {
	var ops []string
	var whitelist []string

	if h.cfg.MCAccess != nil {
		if o, w, err := h.cfg.MCAccess.ListAccess(c.UserContext()); err == nil {
			ops = o
			whitelist = w
		}
	}

	var onlinePlayers []string
	whitelistEnforced := false
	if h.cfg.MCAccess != nil {
		if pl, err := h.cfg.MCAccess.OnlinePlayers(); err == nil {
			onlinePlayers = pl
		}
		if enf, err := h.cfg.MCAccess.WhitelistEnforced(); err == nil {
			whitelistEnforced = enf
		}
	}

	fk, fm := shared.TakeFlash(c)
	return shared.Render(c, pages.MinecraftAccess(ops, whitelist, onlinePlayers, whitelistEnforced, h.cfg.StateStore != nil, fk, fm))
}

// MCAccessGrantOp grants operator status to a player in Git and via live RCON.
func (h *Handler) MCAccessGrantOp(c *fiber.Ctx) error {
	if h.cfg.MCAccess == nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Access manager unconfigured")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}
	user := strings.TrimSpace(c.FormValue("username"))
	if user == "" {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "username is required")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}

	changed, err := h.cfg.MCAccess.GrantOp(c.UserContext(), user, h.cfg.Actor(c))
	if err != nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Grant op failed: "+err.Error())
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if isHTMLForm(c) {
		if changed {
			shared.SetFlash(c, "ok", "Granted operator status to "+user+" — live applied via RCON and committed to Git.")
		} else {
			shared.SetFlash(c, "ok", user+" is already an operator.")
		}
		return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "changed": changed, "user": user})
}

// MCAccessRevokeOp revokes operator status from a player in Git and via live RCON.
func (h *Handler) MCAccessRevokeOp(c *fiber.Ctx) error {
	if h.cfg.MCAccess == nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Access manager unconfigured")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}
	user := strings.TrimSpace(c.FormValue("username"))
	if user == "" {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "username is required")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}

	changed, err := h.cfg.MCAccess.RevokeOp(c.UserContext(), user, h.cfg.Actor(c))
	if err != nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Revoke op failed: "+err.Error())
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if isHTMLForm(c) {
		if changed {
			shared.SetFlash(c, "ok", "Revoked operator status from "+user+" — live applied via RCON and committed to Git.")
		} else {
			shared.SetFlash(c, "ok", user+" is not an operator.")
		}
		return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "changed": changed, "user": user})
}

// MCAccessAddWhitelist adds a player to the server whitelist in Git and via live RCON.
func (h *Handler) MCAccessAddWhitelist(c *fiber.Ctx) error {
	if h.cfg.MCAccess == nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Access manager unconfigured")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}
	user := strings.TrimSpace(c.FormValue("username"))
	if user == "" {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "username is required")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}

	changed, err := h.cfg.MCAccess.AddWhitelist(c.UserContext(), user, h.cfg.Actor(c))
	if err != nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Add whitelist failed: "+err.Error())
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if isHTMLForm(c) {
		if changed {
			shared.SetFlash(c, "ok", "Added "+user+" to whitelist — live applied via RCON and committed to Git.")
		} else {
			shared.SetFlash(c, "ok", user+" is already whitelisted.")
		}
		return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "changed": changed, "user": user})
}

// MCAccessRemoveWhitelist removes a player from the server whitelist in Git and via live RCON.
func (h *Handler) MCAccessRemoveWhitelist(c *fiber.Ctx) error {
	if h.cfg.MCAccess == nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Access manager unconfigured")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}
	user := strings.TrimSpace(c.FormValue("username"))
	if user == "" {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "username is required")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}

	changed, err := h.cfg.MCAccess.RemoveWhitelist(c.UserContext(), user, h.cfg.Actor(c))
	if err != nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Remove whitelist failed: "+err.Error())
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if isHTMLForm(c) {
		if changed {
			shared.SetFlash(c, "ok", "Removed "+user+" from whitelist — live applied via RCON and committed to Git.")
		} else {
			shared.SetFlash(c, "ok", user+" was not in whitelist.")
		}
		return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "changed": changed, "user": user})
}

// MCAccessWhitelistToggle toggles in-game whitelist enforcement via RCON.
func (h *Handler) MCAccessWhitelistToggle(c *fiber.Ctx) error {
	if h.cfg.MCAccess == nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Access manager unconfigured")
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}

	current, err := h.cfg.MCAccess.WhitelistEnforced()
	if err != nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Failed to query whitelist status: "+err.Error())
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	target := !current
	if err := h.cfg.MCAccess.SetWhitelistEnforced(target, h.cfg.Actor(c)); err != nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Failed to toggle whitelist: "+err.Error())
			return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	actionDesc := "enabled"
	if !target {
		actionDesc = "disabled"
	}

	if isHTMLForm(c) {
		shared.SetFlash(c, "ok", fmt.Sprintf("Whitelist enforcement %s via live RCON.", actionDesc))
		return c.Redirect("/minecraft/access", fiber.StatusSeeOther)
	}
	return c.JSON(fiber.Map{"ok": true, "enforced": target})
}
