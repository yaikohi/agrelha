package valheim

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
)

// ValheimInstancePage renders the Valheim instance detail tab view.
func (h *Handler) ValheimInstancePage(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Valheim instance manager not configured")
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}

	tab := strings.ToLower(strings.TrimSpace(c.Params("tab", "overview")))
	if tab == "" {
		tab = "overview"
	}

	inst, err := h.cfg.ValheimInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Valheim instance not found")
	}

	d := pages.InstanceDetailUI{
		InstanceUI: pages.InstanceUI{
			GameID:    string(inst.GameID),
			Number:    inst.Number,
			Name:      inst.Name,
			Slug:      inst.Slug,
			Password:  inst.Password,
			Seed:      inst.Seed,
			Tier:      string(inst.Tier),
			MemoryGiB: inst.MemoryGiB(),
			State:     string(inst.State),
			MOTD:      inst.MOTD,
			LBIP:      inst.LBIP,
		},
		ActiveTab: tab,
	}

	// Fetch installed mods if on mods tab or overview
	if tab == "mods" || tab == "overview" {
		if mods, err := h.cfg.ValheimInstances.GetInstalledMods(c.UserContext(), inst.Number); err == nil {
			d.InstalledMods = mods
		}
	}

	// Fetch config files if on configs tab or overview
	if tab == "configs" || tab == "overview" {
		if cfgs, err := h.cfg.ValheimInstances.ListConfigs(c.UserContext(), inst.Number); err == nil {
			d.ConfigFiles = cfgs
		}
	}

	// Fetch backups if on backups tab
	if tab == "backups" {
		for _, b := range h.cfg.ValheimInstances.ListBackups(*inst) {
			d.Backups = append(d.Backups, pages.BackupUI{
				Name:      b.Name,
				SizeBytes: b.SizeBytes,
				CreatedAt: b.CreatedAt,
			})
		}
	}

	return shared.Render(c, pages.ValheimInstanceDetail(d))
}

// ValheimInstanceSettingsSave saves settings (name, password, tier) for a Valheim instance.
func (h *Handler) ValheimInstanceSettingsSave(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	var req struct {
		Name     string `json:"name" form:"name"`
		Password string `json:"password" form:"password"`
		Tier     string `json:"tier" form:"tier"`
	}
	_ = c.BodyParser(&req)

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.TrimSpace(c.FormValue("name"))
	}
	pwd := strings.TrimSpace(req.Password)
	if pwd == "" {
		pwd = strings.TrimSpace(c.FormValue("password"))
	}
	tierStr := strings.TrimSpace(req.Tier)
	if tierStr == "" {
		tierStr = strings.TrimSpace(c.FormValue("tier"))
	}

	if err := h.cfg.ValheimInstances.UpdateValheimSettings(c.UserContext(), num, name, pwd, tierStr, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Failed to update settings: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", "Settings saved successfully.", nil)
}

// ValheimInstanceModsRemove removes a mod from an instance's mods.txt.
func (h *Handler) ValheimInstanceModsRemove(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	slug := strings.TrimSpace(c.FormValue("slug"))
	if slug == "" {
		return shared.SSEToast(c, "err", "Mod slug required.", nil)
	}

	if err := h.cfg.ValheimInstances.RemoveMod(c.UserContext(), num, slug, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Remove failed: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Removed %s. Updating...", slug), nil)
}

// ValheimInstanceModsInstall installs a mod onto a Valheim instance.
func (h *Handler) ValheimInstanceModsInstall(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	slug := strings.TrimSpace(c.FormValue("slug"))
	if slug == "" {
		return shared.SSEToast(c, "err", "Mod slug required.", nil)
	}

	added, err := h.cfg.ValheimInstances.InstallMod(c.UserContext(), num, slug, h.cfg.Actor(c))
	if err != nil {
		return shared.SSEToast(c, "err", "Install failed: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Installed %s (+%d mods). Updating...", slug, added), nil)
}

// ValheimInstanceExport exports the instance client bundle as an .r2z modpack profile.
func (h *Handler) ValheimInstanceExport(c *fiber.Ctx) error {
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}

	if h.cfg.ValheimInstances == nil || h.cfg.ValheimGame == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Export service unavailable")
	}

	inst, err := h.cfg.ValheimInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Valheim instance not found")
	}

	bundle, err := h.cfg.ValheimGame.ExportClientBundle(c.UserContext(), *inst)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Build .r2z profile failed: " + err.Error())
	}

	c.Set("Content-Type", bundle.ContentType)
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, bundle.Filename))
	return c.Send(bundle.Data)
}
