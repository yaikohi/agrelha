package server

import (
	"github.com/gofiber/fiber/v2"
)

func (s *FiberServer) modsPage(c *fiber.Ctx) error {
	return s.ensureContentHandler().ModsPage(c)
}

func (s *FiberServer) modDetail(c *fiber.Ctx) error {
	return s.ensureContentHandler().ModDetail(c)
}

func (s *FiberServer) modsInstall(c *fiber.Ctx) error {
	return s.ensureContentHandler().ModsInstall(c)
}

func (s *FiberServer) modsRemove(c *fiber.Ctx) error {
	return s.ensureContentHandler().ModsRemove(c)
}
