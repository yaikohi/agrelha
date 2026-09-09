package server

import (
	"github.com/gofiber/fiber/v2"
)

func (s *FiberServer) adminsPage(c *fiber.Ctx) error {
	return s.ensureAccessHandler().AdminsPage(c)
}

func (s *FiberServer) adminsGrant(c *fiber.Ctx) error {
	return s.ensureAccessHandler().AdminsGrant(c)
}

func (s *FiberServer) adminsRevoke(c *fiber.Ctx) error {
	return s.ensureAccessHandler().AdminsRevoke(c)
}
