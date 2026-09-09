package server

import (
	"github.com/gofiber/fiber/v2"
)

func (s *FiberServer) mcInstancePage(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCInstancePage(c)
}

func (s *FiberServer) mcInstanceRcon(c *fiber.Ctx) error {
	return s.ensureConsoleHandler().MCRconCommand(c)
}

func (s *FiberServer) mcInstanceLogsStream(c *fiber.Ctx) error {
	return s.ensureConsoleHandler().MCLogsStream(c)
}

func (s *FiberServer) mcInstanceBackupCreate(c *fiber.Ctx) error {
	return s.ensureBackupsHandler().Create(c)
}

func (s *FiberServer) mcInstanceBackupRestoreInPlace(c *fiber.Ctx) error {
	return s.ensureBackupsHandler().RestoreInPlace(c)
}

func (s *FiberServer) mcInstanceBackupRestoreNew(c *fiber.Ctx) error {
	return s.ensureBackupsHandler().RestoreNew(c)
}

func (s *FiberServer) mcInstanceBackupDownload(c *fiber.Ctx) error {
	return s.ensureBackupsHandler().Download(c)
}

func (s *FiberServer) mcInstanceBackupDelete(c *fiber.Ctx) error {
	return s.ensureBackupsHandler().Delete(c)
}

func (s *FiberServer) mcInstanceSettingsSave(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCInstanceSettingsSave(c)
}

func (s *FiberServer) mcInstanceModsRemove(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCInstanceModsRemove(c)
}

func (s *FiberServer) mcInstanceModsInstall(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCInstanceModsInstall(c)
}

func (s *FiberServer) mcInstanceConfigGet(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCInstanceConfigGet(c)
}

func (s *FiberServer) mcInstanceConfigSave(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCInstanceConfigSave(c)
}

func (s *FiberServer) mcInstanceConfigDelete(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCInstanceConfigDelete(c)
}

func (s *FiberServer) mcInstanceExport(c *fiber.Ctx) error {
	return s.ensureInstancesHandler().MCInstanceExport(c)
}
