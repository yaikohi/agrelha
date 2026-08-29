package server

import (
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/modpack"
	"agrelha/internal/mods"
)

func (s *FiberServer) modpackExport(c *fiber.Ctx) error {
	if s.k8s == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "k8s client not available")
	}
	ctx := c.UserContext()

	data, err := s.k8s.ConfigMapData(ctx, "valheim-mods")
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "read mods: "+err.Error())
	}
	entries := mods.Parse(data["mods.txt"])
	if len(entries) == 0 {
		setFlash(c, "err", "No mods are installed — nothing to export.")
		return c.Redirect("/mods", fiber.StatusSeeOther)
	}

	configs := map[string]string{}
	if cfg, err := s.k8s.ConfigMapData(ctx, configsCM); err == nil {
		for k, v := range cfg {
			configs[k] = v
		}
	}

	blob, err := modpack.Build("Valheim (ykhi)", entries, configs)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "build modpack: "+err.Error())
	}

	_ = s.store.RecordAudit(s.actor(c), "modpack-export", "")

	name := "valheim-" + time.Now().Format("2006-01-02") + ".r2z"
	c.Set("Content-Type", "application/zip")
	c.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	return c.Send(blob)
}
