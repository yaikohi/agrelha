package server

import (
	"github.com/gofiber/fiber/v2"
)

func (s *FiberServer) configsList(c *fiber.Ctx) ([]string, error) {
	return s.ensureContentHandler().ConfigsList(c)
}

func (s *FiberServer) configsPage(c *fiber.Ctx) error {
	return s.ensureContentHandler().ConfigsPage(c)
}

func (s *FiberServer) configNew(c *fiber.Ctx) error {
	return s.ensureContentHandler().ConfigNew(c)
}

func (s *FiberServer) configEdit(c *fiber.Ctx) error {
	return s.ensureContentHandler().ConfigEdit(c)
}

func (s *FiberServer) configSave(c *fiber.Ctx) error {
	return s.ensureContentHandler().ConfigSave(c)
}

func (s *FiberServer) configDelete(c *fiber.Ctx) error {
	return s.ensureContentHandler().ConfigDelete(c)
}
