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
	return render(c, pages.Admins(players, adminIDs, s.admins != nil))
}

func (s *FiberServer) adminsGrant(c *fiber.Ctx) error {
	if s.admins == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "declarative plane disabled (no git token)")
	}
	id := c.FormValue("steam_id")
	if id == "" {
		return fiber.NewError(fiber.StatusBadRequest, "steam_id required")
	}
	if _, err := s.admins.Grant(c.UserContext(), id); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	_ = s.store.RecordAudit(s.actor(c), "admin-grant", id)
	return c.Redirect("/admins", fiber.StatusSeeOther)
}

func (s *FiberServer) adminsRevoke(c *fiber.Ctx) error {
	if s.admins == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "declarative plane disabled (no git token)")
	}
	id := c.FormValue("steam_id")
	if id == "" {
		return fiber.NewError(fiber.StatusBadRequest, "steam_id required")
	}
	if _, err := s.admins.Revoke(c.UserContext(), id); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	_ = s.store.RecordAudit(s.actor(c), "admin-revoke", id)
	return c.Redirect("/admins", fiber.StatusSeeOther)
}
