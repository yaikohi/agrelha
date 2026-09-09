package access

import (
	"strings"

	"agrelha/internal/domain"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"

	"github.com/gofiber/fiber/v2"
)

func (h *Handler) AdminsPage(c *fiber.Ctx) error {
	var players []domain.Player
	if h.cfg.Players != nil {
		players, _ = h.cfg.Players.ListPlayers()
	}
	var adminIDs []string
	if h.cfg.Admins != nil {
		adminIDs, _ = h.cfg.Admins.List(c.UserContext())
	}
	fk, fm := shared.TakeFlash(c)
	return shared.Render(c, pages.Admins(players, adminIDs, h.cfg.Admins != nil, fk, fm))
}

func (h *Handler) AdminsGrant(c *fiber.Ctx) error {
	if h.cfg.Admins == nil {
		shared.SetFlash(c, "err", "Declarative plane disabled — no Git token configured.")
		return c.Redirect("/admins", fiber.StatusSeeOther)
	}
	id := c.FormValue("steam_id")
	if id == "" {
		shared.SetFlash(c, "err", "A Steam64 ID is required.")
		return c.Redirect("/admins", fiber.StatusSeeOther)
	}
	changed, err := h.cfg.Admins.Grant(c.UserContext(), id, h.cfg.Actor(c))
	if err != nil {
		shared.SetFlash(c, "err", "Grant commit failed: "+err.Error())
		return c.Redirect("/admins", fiber.StatusSeeOther)
	}
	if changed {
		if h.cfg.ApplyAfterSync != nil {
			h.cfg.ApplyAfterSync("valheim-admins", "ADMINLIST_IDS", func(v string) bool {
				return strings.Contains(" "+v+" ", " "+id+" ")
			})
		}
		shared.SetFlash(c, "ok", "Granted admin to "+id+" — committed; applies on the next server restart.")
	} else {
		shared.SetFlash(c, "ok", id+" is already an admin.")
	}
	return c.Redirect("/admins", fiber.StatusSeeOther)
}

func (h *Handler) AdminsRevoke(c *fiber.Ctx) error {
	if h.cfg.Admins == nil {
		shared.SetFlash(c, "err", "Declarative plane disabled — no Git token configured.")
		return c.Redirect("/admins", fiber.StatusSeeOther)
	}
	id := c.FormValue("steam_id")
	if id == "" {
		shared.SetFlash(c, "err", "A Steam64 ID is required.")
		return c.Redirect("/admins", fiber.StatusSeeOther)
	}
	changed, err := h.cfg.Admins.Revoke(c.UserContext(), id, h.cfg.Actor(c))
	if err != nil {
		shared.SetFlash(c, "err", "Revoke commit failed: "+err.Error())
		return c.Redirect("/admins", fiber.StatusSeeOther)
	}
	if changed {
		if h.cfg.ApplyAfterSync != nil {
			h.cfg.ApplyAfterSync("valheim-admins", "ADMINLIST_IDS", func(v string) bool {
				return !strings.Contains(" "+v+" ", " "+id+" ")
			})
		}
		shared.SetFlash(c, "ok", "Revoked admin from "+id+" — committed; applies on the next server restart.")
	} else {
		shared.SetFlash(c, "ok", id+" was not an admin.")
	}
	return c.Redirect("/admins", fiber.StatusSeeOther)
}
