package minecraft

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/domain"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
	"agrelha/internal/web/sse"
)

// MCInstancePage renders the instance detail tab view.
func (h *Handler) MCInstancePage(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Instance manager not configured")
	}

	instanceNumber, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}

	tab := strings.ToLower(strings.TrimSpace(c.Params("tab", "overview")))
	if tab == "" {
		tab = "overview"
	}

	inst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), instanceNumber)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Instance not found")
	}

	if tab == "mods" && inst.IsVanilla() {
		shared.SetFlash(c, "err", inst.VanillaImmutableErr().Error())
		return c.Redirect(fmt.Sprintf("/minecraft/%d/overview", instanceNumber), fiber.StatusSeeOther)
	}

	packName := ""
	packRef := ""
	packProvider := ""
	seed := ""
	loader := ""
	mcVersion := ""
	if inst.Minecraft != nil {
		seed = inst.Minecraft.Seed
		loader = string(inst.Minecraft.Loader)
		mcVersion = inst.Minecraft.MCVersion
		if inst.Minecraft.Pack != nil {
			packName = inst.Minecraft.Pack.Name
			packRef = inst.Minecraft.Pack.Ref
			packProvider = string(inst.Minecraft.Pack.Provider)
		}
	}

	d := pages.InstanceDetailUI{
		InstanceUI: pages.InstanceUI{
			Host:            inst.EffectiveHost(),
			HostUnreachable: inst.HostUnreachable,
			HostUnknown:     inst.HostUnknown,
			IsAdmin:         shared.IsAdmin(c),
			CanOperate:      shared.PrincipalOf(c).CanOperate(domain.GameMinecraft, inst.Number),
			Owned:           shared.PrincipalOf(c).HasRole(domain.InstanceRole(domain.GameMinecraft, inst.Number)),
			GameID:          string(domain.GameMinecraft),
			Number:          inst.Number,
			Name:            inst.Name,
			Slug:            inst.Slug,
			Seed:            seed,
			Loader:          loader,
			Source:          string(inst.Source),
			Pack:            packName,
			PackRef:         packRef,
			PackProvider:    packProvider,
			MCVersion:       mcVersion,
			Tier:            string(inst.Tier),
			MemoryGiB:       inst.MemoryGiB(domain.MinecraftProfile),
			State:           string(inst.State),
			MOTD:            inst.MOTD,
			LBIP:            inst.LBIP,
		},
		ActiveTab: tab,
	}
	d.Vanilla = inst.IsVanilla()
	d.FlashKind, d.FlashMsg = shared.TakeFlash(c)
	d.Pack = packName

	if tab == "crashes" && h.cfg.ListIncidents != nil {
		oldestFirst := c.Query("sort") == "oldest"
		d.IncidentsOldestFirst = oldestFirst
		if list, err := h.cfg.ListIncidents(c.UserContext(), inst.WorldID, 200, oldestFirst); err == nil {
			d.Incidents = pages.IncidentViews(list)
		}
	}

	if h.cfg.LastIncident != nil {
		if in, err := h.cfg.LastIncident(c.UserContext(), inst.WorldID); err == nil && in != nil {
			var startedAt time.Time
			if h.cfg.MCInstances != nil {
				if st, serr := h.cfg.MCInstances.RuntimeStatus(c.UserContext(), inst.Number); serr == nil {
					startedAt = st.StartedAt
				}
			}
			if !in.SupersededBy(startedAt) {
				d.LastIncident = pages.IncidentView(in)
			}
		}
	}

	// Fetch installed mods if on mods tab or overview
	if tab == "mods" || tab == "overview" {
		if mods, err := h.cfg.MCInstances.GetInstalledMods(c.UserContext(), inst.Number); err == nil {
			d.InstalledMods = mods
		}
		h.ModUpdateState(c.UserContext(), &d, *inst)
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

	instanceNumber, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	var req struct {
		Name string `json:"name" form:"name"`
		MOTD string `json:"motd" form:"motd"`
		Tier string `json:"tier" form:"tier"`
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
	if err := h.cfg.MCInstances.UpdateSettings(c.UserContext(), instanceNumber, name, motd, tierStr, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Failed to update settings: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", "Settings saved successfully.", nil)
}

// MCInstanceModsRemove removes a mod from an instance's mods.txt.
func (h *Handler) MCInstanceModsRemove(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return shared.SSEToast(c, "err", "Instance manager unconfigured.", nil)
	}

	instanceNumber, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	slug, provider := modRequest(c)
	if slug == "" {
		return shared.SSEToast(c, "err", "Mod slug required.", nil)
	}

	remove := h.cfg.MCInstances.RemoveMod
	if provider == domain.ProviderCurseForge {
		remove = h.cfg.MCInstances.RemoveCurseForgeMod
	}
	if err := remove(c.UserContext(), instanceNumber, slug, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Remove failed: "+err.Error(), nil)
	}

	updatedModsHTML := h.renderMCInstalledModsHTML(c.UserContext(), instanceNumber)
	modsMap := make(map[string]bool)
	if mods, err := h.cfg.MCInstances.GetInstalledMods(c.UserContext(), instanceNumber); err == nil {
		for _, m := range mods {
			modsMap[m] = true
		}
	}
	if cfMods, err := h.cfg.MCInstances.GetCurseForgeMods(c.UserContext(), instanceNumber); err == nil {
		for _, m := range cfMods {
			modsMap[m] = true
		}
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	_ = sse.InnerElement(w, "#installed-mods-container", updatedModsHTML)
	_ = sse.PatchSignals(w, map[string]any{
		"toast":     fmt.Sprintf("Removed %s. Updating...", slug),
		"toastkind": "ok",
		fmt.Sprintf("inst_minecraft_%d_mods", instanceNumber):       modsMap,
		fmt.Sprintf("inst_minecraft_%d_mods_count", instanceNumber): len(modsMap),
	})
	_ = w.Flush()
	return c.Send(buf.Bytes())
}

// MCInstanceModsInstall installs a mod and its dependencies onto an instance.
func (h *Handler) MCInstanceModsInstall(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return shared.SSEToast(c, "err", "Instance manager unconfigured.", nil)
	}
	instanceNumber, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	slug, provider := modRequest(c)
	if slug == "" {
		return shared.SSEToast(c, "err", "Mod slug required.", nil)
	}

	// Routed by Provider, never guessed: the two catalogues store entries in
	// different files in different syntaxes, and a CurseForge file id handed to
	// Modrinth would only fail at boot.
	var msg string
	if provider == domain.ProviderCurseForge {
		added, err := h.cfg.MCInstances.InstallCurseForgeMod(c.UserContext(), instanceNumber, slug, h.cfg.Actor(c))
		if err != nil {
			return shared.SSEToast(c, "err", "Install failed: "+err.Error(), nil)
		}
		msg = fmt.Sprintf("Installed %s from CurseForge. Updating...", slug)
		if added > 1 {
			msg = fmt.Sprintf("Installed %s from CurseForge (+%d required). Updating...", slug, added-1)
		}
	} else {
		added, err := h.cfg.MCInstances.InstallMod(c.UserContext(), instanceNumber, slug, h.cfg.Actor(c))
		if err != nil {
			return shared.SSEToast(c, "err", "Install failed: "+err.Error(), nil)
		}
		msg = fmt.Sprintf("Installed %s (+%d deps). Updating...", slug, added-1)
	}

	updatedModsHTML := h.renderMCInstalledModsHTML(c.UserContext(), instanceNumber)
	modsMap := make(map[string]bool)
	if mods, err := h.cfg.MCInstances.GetInstalledMods(c.UserContext(), instanceNumber); err == nil {
		for _, m := range mods {
			modsMap[m] = true
		}
	}
	if cfMods, err := h.cfg.MCInstances.GetCurseForgeMods(c.UserContext(), instanceNumber); err == nil {
		for _, m := range cfMods {
			modsMap[m] = true
		}
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	_ = sse.InnerElement(w, "#installed-mods-container", updatedModsHTML)
	_ = sse.PatchSignals(w, map[string]any{
		"toast":     msg,
		"toastkind": "ok",
		fmt.Sprintf("inst_minecraft_%d_mods", instanceNumber):       modsMap,
		fmt.Sprintf("inst_minecraft_%d_mods_count", instanceNumber): len(modsMap),
	})
	_ = w.Flush()
	return c.Send(buf.Bytes())
}

func (h *Handler) renderMCInstalledModsHTML(ctx context.Context, instanceNumber int) string {
	var installed []string
	if mods, err := h.cfg.MCInstances.GetInstalledMods(ctx, instanceNumber); err == nil {
		installed = append(installed, mods...)
	}
	if cfMods, err := h.cfg.MCInstances.GetCurseForgeMods(ctx, instanceNumber); err == nil {
		installed = append(installed, cfMods...)
	}
	inst, _ := h.cfg.MCInstances.GetInstance(ctx, instanceNumber)
	d := pages.InstanceDetailUI{
		InstanceUI: pages.InstanceUI{
			GameID:     "minecraft",
			Number:     instanceNumber,
			CanOperate: true,
		},
		InstalledMods: installed,
	}
	if inst != nil && inst.Minecraft != nil && inst.Minecraft.Pack != nil {
		d.Pack = inst.Minecraft.Pack.Name
		d.PackRef = inst.Minecraft.Pack.Ref
	}
	var sb strings.Builder
	_ = pages.InstalledMods(pages.MinecraftInstalledMods(d)).Render(ctx, &sb)
	return sb.String()
}

// MCInstanceExport hands a player this world's client pack: an .mrpack built
// from its mod list, or a redirect to the Pack release the server installed.
func (h *Handler) MCInstanceExport(c *fiber.Ctx) error {
	instanceNumber, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}

	if h.cfg.MCInstances == nil || h.cfg.MinecraftGame == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Export service unavailable")
	}

	inst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), instanceNumber)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Instance not found")
	}

	bundle, err := h.cfg.MinecraftGame.ExportClientBundle(c.UserContext(), *inst)
	switch {
	case errors.Is(err, domain.ErrNothingToExport):
		return c.Status(fiber.StatusConflict).SendString(noPackToExport(*inst))
	case err != nil:
		return c.Status(fiber.StatusInternalServerError).SendString("Build mrpack failed: " + err.Error())
	}

	// A Pack release is the publisher's file, so the player is sent to it rather
	// than served a copy of it through the panel.
	if bundle.URL != "" {
		return c.Redirect(bundle.URL, fiber.StatusTemporaryRedirect)
	}

	c.Set("Content-Type", bundle.ContentType)
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, bundle.Filename))
	return c.Send(bundle.Data)
}

// noPackToExport says why there is nothing to download and where to go instead.
// A player who clicked "get the mods" is owed the next step, not a status code.
func noPackToExport(inst domain.Instance) string {
	pack := ""
	ref := ""
	if inst.Minecraft != nil && inst.Minecraft.Pack != nil {
		pack = strings.TrimSpace(inst.Minecraft.Pack.Name)
		ref = strings.TrimSpace(inst.Minecraft.Pack.Ref)
	}
	switch {
	case pack != "" && ref != "":
		return fmt.Sprintf("%s runs the %s modpack, which its publisher distributes: install it from %s", inst.Name, pack, ref)
	case ref != "":
		return fmt.Sprintf("%s runs a modpack its publisher distributes: install it from %s", inst.Name, ref)
	case pack != "":
		return fmt.Sprintf("%s runs the %s modpack: install that pack in your launcher", inst.Name, pack)
	default:
		return fmt.Sprintf("%s has no mods to export yet", inst.Name)
	}
}

// modRequest reads the slug and Provider a mod action applies to. An absent
// Provider means Modrinth, so links written before CurseForge existed keep
// working.
func modRequest(c *fiber.Ctx) (string, domain.Provider) {
	var req struct {
		Slug     string `json:"slug" form:"slug"`
		Provider string `json:"provider" form:"provider"`
	}
	_ = c.BodyParser(&req)

	slug := strings.TrimSpace(req.Slug)
	for _, fallback := range []string{c.FormValue("slug"), c.Query("slug")} {
		if slug == "" {
			slug = strings.TrimSpace(fallback)
		}
	}

	raw := strings.TrimSpace(req.Provider)
	for _, fallback := range []string{c.FormValue("provider"), c.Query("provider")} {
		if raw == "" {
			raw = strings.TrimSpace(fallback)
		}
	}
	if strings.EqualFold(raw, string(domain.ProviderCurseForge)) {
		return slug, domain.ProviderCurseForge
	}
	return slug, domain.ProviderModrinth
}
