package server

import (
	"context"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/modpack"
	"agrelha/internal/mods"
)

const fallbackBepInExVersion = "5.4.2333"

// withBepInEx guarantees the exported profile carries denikson/BepInExPack_Valheim
// as mod #1. The server's valheim-mods list omits it (the lloesche image installs
// BepInEx itself), but an r2modman profile is unlaunchable without it — no loader
// means the doorstop target is undefined ("[object Object]") and r2modman crashes.
func (s *FiberServer) withBepInEx(ctx context.Context, entries []string) []string {
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e), "bepinexpack") {
			return entries
		}
	}
	ver := fallbackBepInExVersion
	if s.ts != nil {
		if v, _, err := s.ts.LatestVersion(ctx, "denikson", "BepInExPack_Valheim"); err == nil && v != "" {
			ver = v
		}
	}
	return append([]string{"denikson/BepInExPack_Valheim/" + ver}, entries...)
}

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

	entries = s.withBepInEx(ctx, entries)

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
