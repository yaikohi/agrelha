package access

import (
	"errors"
	"fmt"
	"strings"

	mcaccess "agrelha/internal/app/access"

	"agrelha/internal/domain"
	"agrelha/internal/web/shared"

	"github.com/gofiber/fiber/v2"
)

func isHTMLForm(c *fiber.Ctx) bool {
	accept := c.Get("Accept")
	return strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json")
}

const mcRedirectTarget = "/access?game=minecraft"

// applySuffix says what actually happened. Claiming a live apply when the
// server never heard it is how an operator comes to believe a grant landed: the
// panel confirmed it, ops.json stayed empty, and nothing said so until somebody
// looked at the file.
func (h *Handler) applySuffix(err error) string {
	live := h.cfg.MCAccess != nil && h.cfg.MCAccess.AppliesLive() && !errors.Is(err, mcaccess.ErrRecordedOnly)
	if !live {
		return "recorded; it reaches the server the next time the world restarts."
	}
	if h.cfg.HasGit {
		return "applied in-game now and committed to Git."
	}
	return "applied in-game now."
}

// recorded reports a change the server has not heard yet. Refusing it would be
// wrong - it is in agrelha's state and the next start renders it - so it is a
// success that says so.
func recorded(err error) bool { return errors.Is(err, mcaccess.ErrRecordedOnly) }

func (h *Handler) MCAccessPage(c *fiber.Ctx) error {
	return c.Redirect(mcRedirectTarget, fiber.StatusTemporaryRedirect)
}

// MCAccessGrantOp grants operator status to a player in Git and via live RCON.
func (h *Handler) MCAccessGrantOp(c *fiber.Ctx) error {
	if h.cfg.MCAccess == nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Access manager unconfigured")
			return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}
	user := strings.TrimSpace(c.FormValue("username"))
	if err := domain.ValidateMinecraftUsername(user); err != nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", err.Error())
			return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	changed, err := h.cfg.MCAccess.GrantOp(c.UserContext(), user, h.cfg.Actor(c))
	if err != nil && !recorded(err) {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Grant op failed: "+err.Error())
			return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if isHTMLForm(c) {
		if changed {
			shared.SetFlash(c, "ok", "Granted operator status to "+user+" — "+h.applySuffix(err))
		} else {
			shared.SetFlash(c, "ok", user+" is already an operator.")
		}
		return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "changed": changed, "user": user})
}

// MCAccessRevokeOp revokes operator status from a player in Git and via live RCON.
func (h *Handler) MCAccessRevokeOp(c *fiber.Ctx) error {
	if h.cfg.MCAccess == nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Access manager unconfigured")
			return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}
	user := strings.TrimSpace(c.FormValue("username"))
	if err := domain.ValidateMinecraftUsername(user); err != nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", err.Error())
			return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	changed, err := h.cfg.MCAccess.RevokeOp(c.UserContext(), user, h.cfg.Actor(c))
	if err != nil && !recorded(err) {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Revoke op failed: "+err.Error())
			return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if isHTMLForm(c) {
		if changed {
			shared.SetFlash(c, "ok", "Revoked operator status from "+user+" — "+h.applySuffix(err))
		} else {
			shared.SetFlash(c, "ok", user+" is not an operator.")
		}
		return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "changed": changed, "user": user})
}

// MCAccessAddWhitelist adds a player to the server whitelist in Git and via live RCON.
func (h *Handler) MCAccessAddWhitelist(c *fiber.Ctx) error {
	if h.cfg.MCAccess == nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Access manager unconfigured")
			return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}
	user := strings.TrimSpace(c.FormValue("username"))
	if err := domain.ValidateMinecraftUsername(user); err != nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", err.Error())
			return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	changed, err := h.cfg.MCAccess.AddWhitelist(c.UserContext(), user, h.cfg.Actor(c))
	if err != nil && !recorded(err) {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Add whitelist failed: "+err.Error())
			return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if isHTMLForm(c) {
		if changed {
			shared.SetFlash(c, "ok", "Added "+user+" to whitelist — "+h.applySuffix(err))
		} else {
			shared.SetFlash(c, "ok", user+" is already whitelisted.")
		}
		return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "changed": changed, "user": user})
}

// MCAccessRemoveWhitelist removes a player from the server whitelist in Git and via live RCON.
func (h *Handler) MCAccessRemoveWhitelist(c *fiber.Ctx) error {
	if h.cfg.MCAccess == nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Access manager unconfigured")
			return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}
	user := strings.TrimSpace(c.FormValue("username"))
	if err := domain.ValidateMinecraftUsername(user); err != nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", err.Error())
			return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	changed, err := h.cfg.MCAccess.RemoveWhitelist(c.UserContext(), user, h.cfg.Actor(c))
	if err != nil && !recorded(err) {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Remove whitelist failed: "+err.Error())
			return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if isHTMLForm(c) {
		if changed {
			shared.SetFlash(c, "ok", "Removed "+user+" from whitelist — "+h.applySuffix(err))
		} else {
			shared.SetFlash(c, "ok", user+" was not in whitelist.")
		}
		return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
	}

	return c.JSON(fiber.Map{"ok": true, "changed": changed, "user": user})
}

// MCAccessWhitelistToggle turns Admission enforcement on or off. It is the only
// thing that does: no grant and no world creation may close a world.
func (h *Handler) MCAccessWhitelistToggle(c *fiber.Ctx) error {
	if h.cfg.MCAccess == nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Access manager unconfigured")
			return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Access manager unconfigured"})
	}

	current, err := h.cfg.MCAccess.WhitelistEnforced(c.UserContext())
	if err != nil {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Failed to query whitelist status: "+err.Error())
			return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	target := !current
	setErr := h.cfg.MCAccess.SetWhitelistEnforced(c.UserContext(), target, h.cfg.Actor(c))
	if setErr != nil && !recorded(setErr) {
		if isHTMLForm(c) {
			shared.SetFlash(c, "err", "Failed to toggle whitelist: "+setErr.Error())
			return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": setErr.Error()})
	}

	actionDesc := "enabled"
	if !target {
		actionDesc = "disabled"
	}

	if isHTMLForm(c) {
		shared.SetFlash(c, "ok", fmt.Sprintf("Whitelist enforcement %s — %s", actionDesc, h.applySuffix(setErr)))
		return c.Redirect(mcRedirectTarget, fiber.StatusSeeOther)
	}
	return c.JSON(fiber.Map{"ok": true, "enforced": target})
}
