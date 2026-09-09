package server

import (
	"github.com/gofiber/fiber/v2"
)

func (s *FiberServer) mcWizardPage(c *fiber.Ctx) error {
	return s.ensureWizardHandler().MCWizardPage(c)
}

func (s *FiberServer) mcWizardModpacksSearch(c *fiber.Ctx) error {
	return s.ensureWizardHandler().MCWizardModpacksSearch(c)
}

func (s *FiberServer) mcWizardModsSearch(c *fiber.Ctx) error {
	return s.ensureWizardHandler().MCWizardModsSearch(c)
}

func (s *FiberServer) mcWizardCartCheck(c *fiber.Ctx) error {
	return s.ensureWizardHandler().MCWizardCartCheck(c)
}

func (s *FiberServer) mcWizardCreate(c *fiber.Ctx) error {
	return s.ensureWizardHandler().MCWizardCreate(c)
}

func (s *FiberServer) mcProvisioningPage(c *fiber.Ctx) error {
	return s.ensureWizardHandler().MCProvisioningPage(c)
}

func (s *FiberServer) mcProvisioningStream(c *fiber.Ctx) error {
	return s.ensureWizardHandler().MCProvisioningStream(c)
}

func (s *FiberServer) mcWizardImport(c *fiber.Ctx) error {
	return s.ensureWizardHandler().MCWizardImport(c)
}
