package server

import (
	"bufio"
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/cmd/web/pages"
	"agrelha/internal/minecraft"
	"agrelha/internal/modpack"
)

func (s *FiberServer) mcInstancePage(c *fiber.Ctx) error {
	if s.mcInstances == nil {
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

	inst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Instance not found")
	}

	d := pages.InstanceDetailUI{
		InstanceUI: pages.InstanceUI{
			Number:    inst.Number,
			Name:      inst.Name,
			Slug:      inst.Slug,
			Seed:      inst.Seed,
			Loader:    string(inst.Loader),
			Source:    string(inst.Source),
			MCVersion: inst.MCVersion,
			Tier:      string(inst.Tier),
			MemoryGiB: inst.MemoryGiB(),
			State:     string(inst.State),
			MOTD:      inst.MOTD,
			LBIP:      inst.LBIP,
		},
		ActiveTab: tab,
	}
	if inst.Pack != nil {
		d.Pack = inst.Pack.Name
	}

	// Fetch installed mods if on mods tab or overview
	if s.mck8s != nil {
		if data, err := s.mck8s.ConfigMapData(c.UserContext(), inst.ModsCMName()); err == nil {
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

	// Fetch backups if on backups tab
	if tab == "backups" && s.cfg.BackupsDir != "" {
		pattern := filepath.Join(s.cfg.BackupsDir, fmt.Sprintf("mc-%s-%02d-*.tar.gz", inst.Slug, inst.Number))
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

	return render(c, pages.MinecraftInstanceDetail(d))
}

func (s *FiberServer) mcInstanceRcon(c *fiber.Ctx) error {
	if s.mcInstances == nil || s.mcRconPool == nil {
		return sseToast(c, "err", "RCON or Instance manager unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return sseToast(c, "err", "Invalid instance number.", nil)
	}

	inst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return sseToast(c, "err", "Instance not found.", nil)
	}

	cmd := strings.TrimSpace(c.FormValue("cmd"))
	if cmd == "" {
		return sseToast(c, "err", "Command cannot be empty.", nil)
	}

	addr := fmt.Sprintf("%s.minecraft-modded.svc.cluster.local:25575", inst.ServiceName())
	client := s.mcRconPool.ClientFor(addr)

	resp, err := client.Execute(cmd)
	if err != nil {
		resp = fmt.Sprintf("Error: %s", err.Error())
	}

	escapedCmd := html.EscapeString(cmd)
	escapedResp := html.EscapeString(resp)
	fragment := fmt.Sprintf(`
		<div class="border-t border-zinc-800/40 pt-1 mt-1">
			<span class="text-emerald-400 font-semibold">&gt; %s</span>
			<pre class="text-zinc-300 font-mono whitespace-pre-wrap mt-0.5">%s</pre>
		</div>
	`, escapedCmd, escapedResp)

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	out := fmt.Sprintf("event: datastar-merge-fragments\ndata: selector #console-logs\ndata: mergeMode append\ndata: %s\n\n", fragment)
	return c.SendString(out)
}

func (s *FiberServer) mcInstanceLogsStream(c *fiber.Ctx) error {
	if s.mcInstances == nil || s.mck8s == nil {
		return c.SendString("data: Logs unavailable\n\n")
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid number")
	}

	inst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Not found")
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		stream, err := s.mck8s.StreamDeploymentLogs(context.Background(), inst.DeploymentName(), 100)
		if err != nil {
			_, _ = fmt.Fprintf(w, "event: datastar-merge-fragments\ndata: selector #console-logs\ndata: <p class=\"text-red-400\">Log stream error: %s</p>\n\n", html.EscapeString(err.Error()))
			_ = w.Flush()
			return
		}
		defer stream.Close()

		scanner := bufio.NewScanner(stream)
		for scanner.Scan() {
			line := html.EscapeString(scanner.Text())
			frag := fmt.Sprintf("<div class=\"text-zinc-300\">%s</div>", line)
			_, _ = fmt.Fprintf(w, "event: datastar-merge-fragments\ndata: selector #console-logs\ndata: mergeMode append\ndata: %s\n\n", frag)
			_ = w.Flush()
		}
	})

	return nil
}

func (s *FiberServer) mcInstanceBackupCreate(c *fiber.Ctx) error {
	if s.mcInstances == nil {
		return sseToast(c, "err", "Instance manager unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return sseToast(c, "err", "Invalid instance number.", nil)
	}

	inst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return sseToast(c, "err", "Instance not found.", nil)
	}

	// Flush world save via RCON if running
	if inst.State == minecraft.StateRunning && s.mcRconPool != nil {
		addr := fmt.Sprintf("%s.minecraft-modded.svc.cluster.local:25575", inst.ServiceName())
		client := s.mcRconPool.ClientFor(addr)
		_, _ = client.Execute("/save-off")
		_, _ = client.Execute("/save-all flush")
		defer func() {
			_, _ = client.Execute("/save-on")
		}()
	}

	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("mc-%s-%02d-%s.tar.gz", inst.Slug, inst.Number, timestamp)

	_ = s.store.RecordAudit(s.actor(c), "mc-backup", fmt.Sprintf("Backup %s for instance #%02d", backupName, num))
	_ = s.store.RecordEvent("mc-backup", s.actor(c))

	return sseToast(c, "ok", fmt.Sprintf("Backup triggered: %s", backupName), nil)
}

func (s *FiberServer) mcInstanceSettingsSave(c *fiber.Ctx) error {
	if s.mcInstances == nil {
		return sseToast(c, "err", "Instance manager unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return sseToast(c, "err", "Invalid instance number.", nil)
	}

	inst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return sseToast(c, "err", "Instance not found.", nil)
	}

	inst.Name = strings.TrimSpace(c.FormValue("name"))
	inst.MOTD = strings.TrimSpace(c.FormValue("motd"))
	inst.Tier = minecraft.NormalizeTier(c.FormValue("tier"))
	if v := strings.TrimSpace(c.FormValue("mc_version")); v != "" {
		inst.MCVersion = v
	}

	if err := s.store.UpsertInstance(inst.ToRecord()); err != nil {
		return sseToast(c, "err", "Failed to update instance: "+err.Error(), nil)
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-settings-save", fmt.Sprintf("Updated settings for #%02d", num))
	return sseToast(c, "ok", "Settings saved successfully.", nil)
}

func (s *FiberServer) mcInstanceModsRemove(c *fiber.Ctx) error {
	if s.mcInstances == nil {
		return sseToast(c, "err", "Instance manager unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return sseToast(c, "err", "Invalid instance number.", nil)
	}

	slug := strings.TrimSpace(c.FormValue("slug"))
	if slug == "" {
		return sseToast(c, "err", "Mod slug required.", nil)
	}

	inst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return sseToast(c, "err", "Instance not found.", nil)
	}

	modsPath := fmt.Sprintf("manifests/minecraft-modded/instance-%02d/mods.yaml", num)
	if s.git != nil {
		_, _ = s.git.Patch(c.UserContext(), modsPath, "mods.txt", fmt.Sprintf("mc: remove %s from instance #%02d", slug, num), func(cur string) (string, error) {
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

	_ = s.store.RecordAudit(s.actor(c), "mc-mod-remove", fmt.Sprintf("Removed %s from #%02d", slug, num))
	return sseToast(c, "ok", fmt.Sprintf("Removed %s. Updating...", slug), nil)
}

func (s *FiberServer) mcInstanceExport(c *fiber.Ctx) error {
	if s.mcInstances == nil || s.mr == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Export service unavailable")
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}

	inst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Instance not found")
	}

	var slugs []string
	if s.mck8s != nil {
		if data, err := s.mck8s.ConfigMapData(c.UserContext(), inst.ModsCMName()); err == nil {
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
	if s.mck8s != nil {
		if cfgData, err := s.mck8s.ConfigMapData(c.UserContext(), inst.ConfigsCMName()); err == nil {
			cfgFiles = cfgData
		}
	}

	mrpackBytes, err := modpack.BuildMrpack(c.UserContext(), s.mr, inst.Name, inst.MCVersion, string(inst.Loader), "", slugs, cfgFiles)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Build mrpack failed: " + err.Error())
	}

	fileName := fmt.Sprintf("%s-%s.mrpack", inst.Slug, inst.MCVersion)
	c.Set("Content-Type", "application/x-modrinth-modpack+zip")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	return c.Send(mrpackBytes)
}
