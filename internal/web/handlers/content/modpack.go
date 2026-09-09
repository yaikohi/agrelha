package content

import (
	"context"
	"maps"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/games/valheim"
	"agrelha/internal/app/modpack"
	"agrelha/internal/app/mods"
	"agrelha/internal/domain"
	"agrelha/internal/web/shared"
)

const configsCM = "valheim-mod-configs"

// WithBepInEx delegates to internal/games/valheim to guarantee the exported
// profile carries BepInExPack_Valheim as mod #1.
func (h *Handler) WithBepInEx(ctx context.Context, entries []string) []string {
	ver := ""
	if h.cfg.TS != nil {
		if v, _, err := h.cfg.TS.LatestVersion(ctx, "denikson", "BepInExPack_Valheim"); err == nil && v != "" {
			ver = v
		}
	}
	return valheim.WithBepInEx(entries, ver)
}

// ModpackExport creates and streams a .r2z profile archive for Valheim.
func (h *Handler) ModpackExport(c *fiber.Ctx) error {
	ctx := c.UserContext()

	if h.cfg.ValheimGame != nil {
		bundle, err := h.cfg.ValheimGame.ExportClientBundle(ctx, domain.Instance{Name: "Valheim (ykhi)", Slug: "valheim"})
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "build modpack: "+err.Error())
		}
		if len(bundle.Data) == 0 {
			shared.SetFlash(c, "err", "No mods are installed — nothing to export.")
			return c.Redirect("/mods", fiber.StatusSeeOther)
		}
		if h.cfg.Store != nil {
			_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "modpack-export", "")
		}
		c.Set("Content-Type", bundle.ContentType)
		c.Set("Content-Disposition", `attachment; filename="`+bundle.Filename+`"`)
		return c.Send(bundle.Data)
	}

	if h.cfg.K8s == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "k8s client not available")
	}

	data, err := h.cfg.K8s.ConfigMapData(ctx, "valheim-mods")
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "read mods: "+err.Error())
	}
	entries := mods.Parse(data["mods.txt"])
	if len(entries) == 0 {
		shared.SetFlash(c, "err", "No mods are installed — nothing to export.")
		return c.Redirect("/mods", fiber.StatusSeeOther)
	}

	entries = h.WithBepInEx(ctx, entries)

	configs := map[string]string{}
	if cfg, err := h.cfg.K8s.ConfigMapData(ctx, configsCM); err == nil {
		maps.Copy(configs, cfg)
	}

	blob, err := modpack.Build("Valheim (ykhi)", entries, configs)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "build modpack: "+err.Error())
	}

	if h.cfg.Store != nil {
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "modpack-export", "")
	}

	name := "valheim-" + time.Now().Format("2006-01-02") + ".r2z"
	c.Set("Content-Type", "application/zip")
	c.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	return c.Send(blob)
}
