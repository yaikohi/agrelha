package server

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/cmd/web/pages"
)

func (s *FiberServer) adminsPage(c *fiber.Ctx) error {
	players, _ := s.store.ListPlayers()
	var adminIDs []string
	if s.k8s != nil {
		if data, err := s.k8s.ConfigMapData(c.UserContext(), "valheim-admins"); err == nil {
			adminIDs = strings.Fields(data["ADMINLIST_IDS"])
		}
	}
	fk, fm := takeFlash(c)
	return render(c, pages.Admins(players, adminIDs, s.admins != nil, fk, fm))
}

func (s *FiberServer) adminsGrant(c *fiber.Ctx) error {
	if s.admins == nil {
		setFlash(c, "err", "Declarative plane disabled — no Codeberg token configured.")
		return c.Redirect("/admins", fiber.StatusSeeOther)
	}
	id := c.FormValue("steam_id")
	if id == "" {
		setFlash(c, "err", "A Steam64 ID is required.")
		return c.Redirect("/admins", fiber.StatusSeeOther)
	}
	changed, err := s.admins.Grant(c.UserContext(), id)
	if err != nil {
		setFlash(c, "err", "Grant commit failed: "+err.Error())
		return c.Redirect("/admins", fiber.StatusSeeOther)
	}
	_ = s.store.RecordAudit(s.actor(c), "admin-grant", id)
	if changed {
		s.applyAfterSync("valheim-admins", "ADMINLIST_IDS", func(v string) bool {
			return strings.Contains(" "+v+" ", " "+id+" ")
		})
		setFlash(c, "ok", "Granted admin to "+id+" — committed; applies on the next server restart.")
	} else {
		setFlash(c, "ok", id+" is already an admin.")
	}
	return c.Redirect("/admins", fiber.StatusSeeOther)
}

func (s *FiberServer) adminsRevoke(c *fiber.Ctx) error {
	if s.admins == nil {
		setFlash(c, "err", "Declarative plane disabled — no Codeberg token configured.")
		return c.Redirect("/admins", fiber.StatusSeeOther)
	}
	id := c.FormValue("steam_id")
	if id == "" {
		setFlash(c, "err", "A Steam64 ID is required.")
		return c.Redirect("/admins", fiber.StatusSeeOther)
	}
	changed, err := s.admins.Revoke(c.UserContext(), id)
	if err != nil {
		setFlash(c, "err", "Revoke commit failed: "+err.Error())
		return c.Redirect("/admins", fiber.StatusSeeOther)
	}
	_ = s.store.RecordAudit(s.actor(c), "admin-revoke", id)
	if changed {
		s.applyAfterSync("valheim-admins", "ADMINLIST_IDS", func(v string) bool {
			return !strings.Contains(" "+v+" ", " "+id+" ")
		})
		setFlash(c, "ok", "Revoked admin from "+id+" — committed; applies on the next server restart.")
	} else {
		setFlash(c, "ok", id+" was not an admin.")
	}
	return c.Redirect("/admins", fiber.StatusSeeOther)
}
