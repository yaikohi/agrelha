package instances

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
)

// MCInstancePage renders the instance detail tab view.
func (h *Handler) MCInstancePage(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Instance manager not configured")
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}

	tab := strings.ToLower(strings.TrimSpace(c.Params("tab", "overview")))
	if tab == "" {
		tab = "overview"
	}

	inst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Instance not found")
	}

	packName := ""
	packRef := ""
	packProvider := ""
	if inst.Pack != nil {
		packName = inst.Pack.Name
		packRef = inst.Pack.Ref
		packProvider = string(inst.Pack.Provider)
	}

	d := pages.InstanceDetailUI{
		InstanceUI: pages.InstanceUI{
			Number:       inst.Number,
			Name:         inst.Name,
			Slug:         inst.Slug,
			Seed:         inst.Seed,
			Loader:       string(inst.Loader),
			Source:       string(inst.Source),
			Pack:         packName,
			PackRef:      packRef,
			PackProvider: packProvider,
			MCVersion:    inst.MCVersion,
			Tier:         string(inst.Tier),
			MemoryGiB:    inst.MemoryGiB(),
			State:        string(inst.State),
			MOTD:         inst.MOTD,
			LBIP:         inst.LBIP,
		},
		ActiveTab: tab,
	}
	d.Pack = packName

	if h.cfg.LastIncident != nil {
		if in, err := h.cfg.LastIncident(c.UserContext(), inst.Number); err == nil {
			d.LastIncident = pages.IncidentView(in)
		}
	}

	// Fetch installed mods if on mods tab or overview
	if tab == "mods" || tab == "overview" {
		if mods, err := h.cfg.MCInstances.GetInstalledMods(c.UserContext(), inst.Number); err == nil {
			d.InstalledMods = mods
		}
	}

	// Fetch config files if on configs tab or overview
	if tab == "configs" || tab == "overview" {
		if cfgs, err := h.cfg.MCInstances.ListConfigs(c.UserContext(), inst.Number); err == nil {
			d.ConfigFiles = cfgs
		}
	}

	// Fetch backups if on backups tab
	if tab == "backups" {
		for _, b := range h.cfg.MCInstances.ListBackups(*inst) {
			d.Backups = append(d.Backups, pages.BackupUI{
				Name:      b.Name,
				SizeBytes: b.SizeBytes,
				CreatedAt: b.CreatedAt,
			})
		}
	}

	return shared.Render(c, pages.MinecraftInstanceDetail(d))
}

// MCInstanceSettingsSave saves settings (name, MOTD, tier, MC version) for an instance.
func (h *Handler) MCInstanceSettingsSave(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return shared.SSEToast(c, "err", "Instance manager unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	var req struct {
		Name      string `json:"name" form:"name"`
		MOTD      string `json:"motd" form:"motd"`
		Tier      string `json:"tier" form:"tier"`
		MCVersion string `json:"mc_version" form:"mc_version"`
	}
	_ = c.BodyParser(&req)

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.TrimSpace(c.FormValue("name"))
	}
	motd := strings.TrimSpace(req.MOTD)
	if motd == "" {
		motd = strings.TrimSpace(c.FormValue("motd"))
	}
	tierStr := strings.TrimSpace(req.Tier)
	if tierStr == "" {
		tierStr = strings.TrimSpace(c.FormValue("tier"))
	}
	mcVer := strings.TrimSpace(req.MCVersion)
	if mcVer == "" {
		mcVer = strings.TrimSpace(c.FormValue("mc_version"))
	}

	if err := h.cfg.MCInstances.UpdateSettings(c.UserContext(), num, name, motd, tierStr, mcVer, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Failed to update settings: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", "Settings saved successfully.", nil)
}

// MCInstanceModsRemove removes a mod from an instance's mods.txt.
func (h *Handler) MCInstanceModsRemove(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return shared.SSEToast(c, "err", "Instance manager unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	var req struct {
		Slug string `json:"slug" form:"slug"`
	}
	_ = c.BodyParser(&req)

	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = strings.TrimSpace(c.FormValue("slug"))
	}
	if slug == "" {
		slug = strings.TrimSpace(c.Query("slug"))
	}
	if slug == "" {
		return shared.SSEToast(c, "err", "Mod slug required.", nil)
	}

	if err := h.cfg.MCInstances.RemoveMod(c.UserContext(), num, slug, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Remove failed: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Removed %s. Updating...", slug), nil)
}

// MCInstanceModsInstall installs a mod and its dependencies onto an instance.
func (h *Handler) MCInstanceModsInstall(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return shared.SSEToast(c, "err", "Instance manager unconfigured.", nil)
	}
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	var req struct {
		Slug string `json:"slug" form:"slug"`
	}
	_ = c.BodyParser(&req)

	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = strings.TrimSpace(c.FormValue("slug"))
	}
	if slug == "" {
		slug = strings.TrimSpace(c.Query("slug"))
	}
	if slug == "" {
		return shared.SSEToast(c, "err", "Mod slug required.", nil)
	}

	added, err := h.cfg.MCInstances.InstallMod(c.UserContext(), num, slug, h.cfg.Actor(c))
	if err != nil {
		return shared.SSEToast(c, "err", "Install failed: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Installed %s (+%d deps). Updating...", slug, added-1), nil)
}

// MCInstanceExport exports the instance as a Modrinth modpack (.mrpack).
func (h *Handler) MCInstanceExport(c *fiber.Ctx) error {
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}

	if h.cfg.MCInstances == nil || h.cfg.MinecraftGame == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Export service unavailable")
	}

	inst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Instance not found")
	}

	bundle, err := h.cfg.MinecraftGame.ExportClientBundle(c.UserContext(), *inst)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Build mrpack failed: " + err.Error())
	}
	c.Set("Content-Type", bundle.ContentType)
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, bundle.Filename))
	return c.Send(bundle.Data)
}
