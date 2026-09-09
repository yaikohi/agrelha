package server

import (
	"github.com/gofiber/fiber/v2"
)

func (s *FiberServer) mcConfigsList(c *fiber.Ctx) ([]string, error) {
	return s.ensureInstancesHandler().MCConfigsList(c)
}

func (s *FiberServer) mcConfigsPage(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCConfigsPage(c)
}

func (s *FiberServer) mcConfigNew(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCConfigNew(c)
}

func (s *FiberServer) mcConfigEdit(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCConfigEdit(c)
}

func (s *FiberServer) mcConfigSave(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCConfigSave(c)
}

func (s *FiberServer) mcConfigDelete(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCConfigDelete(c)
}
