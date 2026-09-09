package server

import (
	"github.com/gofiber/fiber/v2"
)

func (s *FiberServer) mcDashboard(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCDashboard(c)
}

func (s *FiberServer) mcInstanceCreate(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCInstanceCreate(c)
}

func (s *FiberServer) mcInstanceStart(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCInstanceStart(c)
}

func (s *FiberServer) mcInstanceStop(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCInstanceStop(c)
}

func (s *FiberServer) mcInstanceDelete(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCInstanceDelete(c)
}

func (s *FiberServer) mcInstanceRestart(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCInstanceRestart(c)
}
