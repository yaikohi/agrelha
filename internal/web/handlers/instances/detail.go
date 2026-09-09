package instances

import (
	"agrelha/internal/infra/store"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/minecraft"
	"agrelha/internal/modpack"
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

	// Fetch installed mods if on mods tab or overview
	if h.cfg.MCK8s != nil {
		if data, err := h.cfg.MCK8s.ConfigMapData(c.UserContext(), inst.ModsCMName()); err == nil {
			if modsTxt, ok := data["mods.txt"]; ok {
				for _, line := range strings.Split(modsTxt, "\n") {
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "#") {
						d.InstalledMods = append(d.InstalledMods, strings.TrimSuffix(line, "?"))
					}
				}
			}
		}
	}

	// Fetch config files if on configs tab or overview
	if (tab == "configs" || tab == "overview") && h.cfg.MCK8s != nil {
		if data, err := h.cfg.MCK8s.ConfigMapData(c.UserContext(), inst.ConfigsCMName()); err == nil {
			for k := range data {
				d.ConfigFiles = append(d.ConfigFiles, k)
			}
			sort.Strings(d.ConfigFiles)
		}
	}

	// Fetch backups if on backups tab
	if tab == "backups" && h.cfg.Cfg != nil && h.cfg.Cfg.BackupsDir != "" {
		pattern := filepath.Join(h.cfg.Cfg.BackupsDir, fmt.Sprintf("mc-%s-%02d-*.tar.gz", inst.Slug, inst.Number))
		if matches, err := filepath.Glob(pattern); err == nil {
			for _, match := range matches {
				if fi, err := os.Stat(match); err == nil {
					d.Backups = append(d.Backups, pages.BackupUI{
						Name:      filepath.Base(match),
						SizeBytes: fi.Size(),
						CreatedAt: fi.ModTime().Format("2006-01-02 15:04"),
					})
				}
			}
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

	inst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return shared.SSEToast(c, "err", "Instance not found.", nil)
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

	if name != "" {
		inst.Name = name
	}
	inst.MOTD = motd
	inst.Tier = minecraft.NormalizeTier(tierStr)

	if mcVer != "" && mcVer != inst.MCVersion {
		if !inst.CanSetVersion() {
			return shared.SSEToast(c, "err", inst.PackOwnedFieldErr("Minecraft version").Error(), nil)
		}
		inst.MCVersion = mcVer
	}

	if h.cfg.Store != nil {
		if err := store.NewInstanceRepo(h.cfg.Store).Upsert(*inst); err != nil {
			return shared.SSEToast(c, "err", "Failed to update instance: "+err.Error(), nil)
		}
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "mc-settings-save", fmt.Sprintf("Updated settings for #%02d", num))
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

	slug := strings.TrimSpace(c.FormValue("slug"))
	if slug == "" {
		return shared.SSEToast(c, "err", "Mod slug required.", nil)
	}

	inst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return shared.SSEToast(c, "err", "Instance not found.", nil)
	}

	modsPath := fmt.Sprintf("manifests/minecraft-modded/instance-%02d/mods.yaml", num)
	if h.cfg.Git != nil {
		_, _ = h.cfg.Git.Patch(c.UserContext(), modsPath, "mods.txt", fmt.Sprintf("mc: remove %s from instance #%02d", slug, num), func(cur string) (string, error) {
			lines := strings.Split(cur, "\n")
			var out []string
			for _, l := range lines {
				if strings.TrimSpace(strings.TrimSuffix(l, "?")) != slug {
					out = append(out, l)
				}
			}
			return strings.Join(out, "\n"), nil
		})
	}

	if h.cfg.Store != nil {
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "mc-mod-remove", fmt.Sprintf("Removed %s from #%02d", slug, num))
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
	slug := strings.TrimSpace(c.FormValue("slug"))
	if slug == "" {
		return shared.SSEToast(c, "err", "Mod slug required.", nil)
	}
	if h.cfg.Git == nil {
		return shared.SSEToast(c, "err", "GitOps plane disabled — no Codeberg token configured.", nil)
	}

	inst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return shared.SSEToast(c, "err", "Instance not found.", nil)
	}

	wanted := []string{slug}
	if h.cfg.MR != nil {
		deps, err := h.cfg.MR.ResolveRequiredDependencies(c.UserContext(), slug, inst.MCVersion, string(inst.Loader))
		if err != nil {
			slog.Warn("could not resolve all mod dependencies", "slug", slug, "instance", num, "err", err)
		}
		wanted = append(wanted, deps...)
	}

	modsPath := fmt.Sprintf("manifests/minecraft-modded/instance-%02d/mods.yaml", num)
	msg := fmt.Sprintf("mc: install %s into instance #%02d", slug, num)
	_, err = h.cfg.Git.Patch(c.UserContext(), modsPath, "mods.txt", msg, func(cur string) (string, error) {
		present := map[string]bool{}
		for _, l := range strings.Split(cur, "\n") {
			if t := strings.TrimSpace(strings.TrimSuffix(l, "?")); t != "" && !strings.HasPrefix(t, "#") {
				present[t] = true
			}
		}
		body := strings.TrimRight(cur, "\n")
		added := 0
		for _, w := range wanted {
			if w = strings.TrimSpace(w); w != "" && !present[w] {
				body += "\n" + w
				present[w] = true
				added++
			}
		}
		if added == 0 {
			return cur, nil
		}
		return strings.TrimLeft(body, "\n") + "\n", nil
	})
	if err != nil {
		return shared.SSEToast(c, "err", "Install failed: "+err.Error(), nil)
	}

	if h.cfg.Store != nil {
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "mc-mod-install", fmt.Sprintf("Installed %s into #%02d", slug, num))
	}
	return shared.SSEToast(c, "ok", fmt.Sprintf("Installed %s (+%d deps). Updating...", slug, len(wanted)-1), nil)
}

// MCInstanceExport exports the instance as a Modrinth modpack (.mrpack).
func (h *Handler) MCInstanceExport(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil || h.cfg.MR == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Export service unavailable")
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}

	inst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Instance not found")
	}

	var slugs []string
	if h.cfg.MCK8s != nil {
		if data, err := h.cfg.MCK8s.ConfigMapData(c.UserContext(), inst.ModsCMName()); err == nil {
			if modsTxt, ok := data["mods.txt"]; ok {
				for _, line := range strings.Split(modsTxt, "\n") {
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "#") {
						slugs = append(slugs, strings.TrimSuffix(line, "?"))
					}
				}
			}
		}
	}

	cfgFiles := make(map[string]string)
	if h.cfg.MCK8s != nil {
		if cfgData, err := h.cfg.MCK8s.ConfigMapData(c.UserContext(), inst.ConfigsCMName()); err == nil {
			cfgFiles = cfgData
		}
	}

	mrpackBytes, err := modpack.BuildMrpack(c.UserContext(), h.cfg.MR, inst.Name, inst.MCVersion, string(inst.Loader), "", slugs, cfgFiles)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Build mrpack failed: " + err.Error())
	}

	fileName := fmt.Sprintf("%s-%s.mrpack", inst.Slug, inst.MCVersion)
	c.Set("Content-Type", "application/x-modrinth-modpack+zip")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	return c.Send(mrpackBytes)
}
