package server

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"html"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/cmd/web/pages"
	"agrelha/internal/minecraft"
	"agrelha/internal/modpack"
	"agrelha/internal/sse"
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

	// Fetch config files if on configs tab or overview
	if (tab == "configs" || tab == "overview") && s.mck8s != nil {
		if data, err := s.mck8s.ConfigMapData(c.UserContext(), inst.ConfigsCMName()); err == nil {
			for k := range data {
				d.ConfigFiles = append(d.ConfigFiles, k)
			}
			sort.Strings(d.ConfigFiles)
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

	var body struct {
		Cmd string `json:"cmd" form:"cmd"`
	}
	_ = c.BodyParser(&body)
	cmd := strings.TrimSpace(body.Cmd)
	if cmd == "" {
		cmd = strings.TrimSpace(c.FormValue("cmd"))
	}
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

	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	_ = sse.AppendElement(w, "#console-logs", fragment)
	return c.Send(buf.Bytes())
}

func (s *FiberServer) mcInstanceLogsStream(c *fiber.Ctx) error {
	if s.mcInstances == nil || s.mck8s == nil {
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		var buf bytes.Buffer
		w := bufio.NewWriter(&buf)
		_ = sse.AppendElement(w, "#console-logs", "<p class=\"text-zinc-500\">Logs unavailable</p>")
		return c.Send(buf.Bytes())
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
			_ = sse.AppendElement(w, "#console-logs", fmt.Sprintf("<p class=\"text-red-400\">Log stream error: %s</p>", html.EscapeString(err.Error())))
			return
		}
		defer stream.Close()

		scanner := bufio.NewScanner(stream)
		for scanner.Scan() {
			line := html.EscapeString(scanner.Text())
			frag := fmt.Sprintf("<div class=\"text-zinc-300\">%s</div>", line)
			_ = sse.AppendElement(w, "#console-logs", frag)
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
	jobName := fmt.Sprintf("mc-bkp-%s-%d-%s", inst.Slug, inst.Number, time.Now().Format("150405"))

	if s.mck8s != nil {
		dataPVC := fmt.Sprintf("mc-instance-%02d-data", inst.Number)
		backupsPVC := "minecraft-modded-backups"
		if err := s.mck8s.CreateBackupJob(c.UserContext(), jobName, backupName, dataPVC, backupsPVC); err != nil {
			return sseToast(c, "err", fmt.Sprintf("Failed to launch backup Job: %s", err.Error()), nil)
		}
	}

	if s.cfg != nil && s.cfg.BackupsDir != "" {
		_ = minecraft.PruneBackups(s.cfg.BackupsDir, inst.Slug, inst.Number, 5)
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-backup", fmt.Sprintf("Backup %s for instance #%02d", backupName, num))
	_ = s.store.RecordEvent("mc-backup", s.actor(c))

	return sseToast(c, "ok", fmt.Sprintf("Backup job started: %s. Archiving to NAS...", backupName), nil)
}

func (s *FiberServer) mcInstanceBackupRestoreInPlace(c *fiber.Ctx) error {
	if s.mcInstances == nil || s.mck8s == nil {
		return sseToast(c, "err", "Instance manager or cluster client unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return sseToast(c, "err", "Invalid instance number.", nil)
	}

	inst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return sseToast(c, "err", "Instance not found.", nil)
	}

	if inst.State == minecraft.StateRunning {
		return sseToast(c, "err", "Cannot restore while world is running. Please stop the server first.", nil)
	}

	var req struct {
		Archive string `json:"archive" form:"archive"`
	}
	_ = c.BodyParser(&req)
	archiveName := filepath.Base(strings.TrimSpace(req.Archive))
	if archiveName == "" || archiveName == "." || !strings.HasSuffix(archiveName, ".tar.gz") {
		return sseToast(c, "err", "Valid backup archive name required.", nil)
	}

	// 1. Take pre-restore safety snapshot
	safetyArchive := minecraft.FormatBackupFileName(inst.Slug, inst.Number, "prerestore")
	safetyJob := fmt.Sprintf("mc-bkp-%s-%d-%s", inst.Slug, inst.Number, time.Now().Format("150405"))
	_ = s.mck8s.CreateBackupJob(c.UserContext(), safetyJob, safetyArchive, inst.PVCName(), "minecraft-modded-backups")

	// 2. Launch restore job
	restoreJobName := fmt.Sprintf("mc-rst-%s-%d-%s", inst.Slug, inst.Number, time.Now().Format("150405"))
	if err := s.mck8s.CreateRestoreJob(c.UserContext(), restoreJobName, archiveName, inst.PVCName(), "minecraft-modded-backups"); err != nil {
		return sseToast(c, "err", "Failed to launch restore Job: "+err.Error(), nil)
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-backup-restore-inplace", fmt.Sprintf("Restored %s into #%02d", archiveName, num))
	_ = s.store.RecordEvent("mc-backup-restore-inplace", s.actor(c))

	return sseToast(c, "ok", fmt.Sprintf("In-place restore started from %s (safety snapshot saved). World data is unpacking.", archiveName), nil)
}

func (s *FiberServer) mcInstanceBackupRestoreNew(c *fiber.Ctx) error {
	if s.mcInstances == nil || s.mck8s == nil {
		return sseToast(c, "err", "Instance manager or cluster client unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return sseToast(c, "err", "Invalid instance number.", nil)
	}

	srcInst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err != nil || srcInst == nil {
		return sseToast(c, "err", "Source instance not found.", nil)
	}

	var req struct {
		Name    string `json:"name" form:"name"`
		Tier    string `json:"tier" form:"tier"`
		Archive string `json:"archive" form:"archive"`
	}
	_ = c.BodyParser(&req)

	newName := strings.TrimSpace(req.Name)
	if newName == "" {
		newName = fmt.Sprintf("%s Restored", srcInst.Name)
	}
	archiveName := filepath.Base(strings.TrimSpace(req.Archive))
	if archiveName == "" || archiveName == "." || !strings.HasSuffix(archiveName, ".tar.gz") {
		return sseToast(c, "err", "Valid backup archive name required.", nil)
	}

	newTier := srcInst.Tier
	if req.Tier != "" {
		newTier = minecraft.NormalizeTier(req.Tier)
	}

	newInst := minecraft.Instance{
		Name:       newName,
		Seed:       srcInst.Seed,
		Loader:     srcInst.Loader,
		Source:     srcInst.Source,
		Pack:       srcInst.Pack,
		MCVersion:  srcInst.MCVersion,
		Tier:       newTier,
		MOTD:       fmt.Sprintf("%s (Restored)", newName),
		Difficulty: srcInst.Difficulty,
		Gamemode:   srcInst.Gamemode,
		WorldType:  srcInst.WorldType,
		State:      minecraft.StateStopped,
	}

	modsTxt := ""
	if data, err := s.mck8s.ConfigMapData(c.UserContext(), srcInst.ModsCMName()); err == nil {
		modsTxt = data["mods.txt"]
	}

	created, err := s.mcInstances.CreateInstance(c.UserContext(), newInst, modsTxt)
	if err != nil {
		return sseToast(c, "err", "Failed to create new instance: "+err.Error(), nil)
	}

	restoreJobName := fmt.Sprintf("mc-rst-%s-%d-%s", created.Slug, created.Number, time.Now().Format("150405"))
	if err := s.mck8s.CreateRestoreJob(c.UserContext(), restoreJobName, archiveName, created.PVCName(), "minecraft-modded-backups"); err != nil {
		return sseToast(c, "err", "Instance created, but restore Job failed: "+err.Error(), nil)
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-backup-restore-new", fmt.Sprintf("Restored %s into new instance #%02d %q", archiveName, created.Number, created.Name))
	_ = s.store.RecordEvent("mc-backup-restore-new", s.actor(c))

	return sseToast(c, "ok", fmt.Sprintf("World %q created from backup! Redirecting...", created.Name), map[string]any{
		"redirect": fmt.Sprintf("/minecraft/provisioning/%d", created.Number),
	})
}

func (s *FiberServer) mcInstanceBackupDownload(c *fiber.Ctx) error {
	if s.cfg == nil || s.cfg.BackupsDir == "" {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Backups directory unconfigured")
	}

	fileName := filepath.Base(strings.TrimSpace(c.Query("f")))
	if fileName == "" || fileName == "." || !strings.HasSuffix(fileName, ".tar.gz") {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid backup file")
	}

	filePath := filepath.Join(s.cfg.BackupsDir, fileName)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return c.Status(fiber.StatusNotFound).SendString("Backup file not found")
	}

	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	return c.SendFile(filePath)
}

func (s *FiberServer) mcInstanceBackupDelete(c *fiber.Ctx) error {
	if s.cfg == nil || s.cfg.BackupsDir == "" {
		return sseToast(c, "err", "Backups directory unconfigured.", nil)
	}

	var req struct {
		File string `json:"file" form:"file"`
	}
	_ = c.BodyParser(&req)

	fileName := filepath.Base(strings.TrimSpace(req.File))
	if fileName == "" || fileName == "." {
		return sseToast(c, "err", "File name required.", nil)
	}

	if err := minecraft.DeleteBackup(s.cfg.BackupsDir, fileName); err != nil {
		return sseToast(c, "err", "Failed to delete backup: "+err.Error(), nil)
	}

	num, _ := strconv.Atoi(c.Params("num"))
	_ = s.store.RecordAudit(s.actor(c), "mc-backup-delete", fmt.Sprintf("Deleted backup %s for #%02d", fileName, num))

	return sseToast(c, "ok", fmt.Sprintf("Deleted %s.", fileName), map[string]any{
		"redirect": fmt.Sprintf("/minecraft/%d/backups", num),
	})
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

	// The Minecraft version belongs to the Pack when there is one. Refuse rather
	// than silently writing a version the pack was never built for.
	if mcVer != "" && mcVer != inst.MCVersion {
		if !inst.CanSetVersion() {
			return sseToast(c, "err", inst.PackOwnedFieldErr("Minecraft version").Error(), nil)
		}
		inst.MCVersion = mcVer
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

func (s *FiberServer) mcInstanceConfigGet(c *fiber.Ctx) error {
	if s.mcInstances == nil || s.mck8s == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Service unavailable")
	}
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}
	inst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Instance not found")
	}

	fileName := strings.TrimSpace(c.Query("f"))
	if fileName == "" {
		return c.Status(fiber.StatusBadRequest).SendString("File name required")
	}

	content := ""
	if data, err := s.mck8s.ConfigMapData(c.UserContext(), inst.ConfigsCMName()); err == nil {
		content = data[fileName]
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	signals := map[string]any{
		"selectedFile": fileName,
		"fileContent":  content,
		"isNew":        false,
		"showEditor":   true,
	}
	if err := sse.PatchSignals(w, signals); err != nil {
		return err
	}
	return c.Send(buf.Bytes())
}

func (s *FiberServer) mcInstanceConfigSave(c *fiber.Ctx) error {
	if s.mcInstances == nil || s.git == nil {
		return sseToast(c, "err", "Instance manager or Git committer unconfigured.", nil)
	}
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return sseToast(c, "err", "Invalid instance number.", nil)
	}
	inst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return sseToast(c, "err", "Instance not found.", nil)
	}

	var req struct {
		File    string `json:"file" form:"file"`
		Content string `json:"content" form:"content"`
	}
	_ = c.BodyParser(&req)

	fileName := strings.TrimSpace(req.File)
	if fileName == "" {
		fileName = strings.TrimSpace(c.FormValue("file"))
	}
	if !mcCfgNameRe.MatchString(fileName) {
		return sseToast(c, "err", "Invalid config file name. Must end in .toml, .json, .yaml, .cfg, .snbt, etc.", nil)
	}

	content := strings.ReplaceAll(req.Content, "\r\n", "\n")
	relPath := fmt.Sprintf("manifests/minecraft-modded/instance-%02d/configs.yaml", inst.Number)

	changed, err := s.git.SetData(c.UserContext(), relPath, fileName, content,
		fmt.Sprintf("agrelha: edit config %s for instance #%02d", fileName, inst.Number))
	if err != nil {
		return sseToast(c, "err", "Save failed: "+err.Error(), nil)
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-config-edit", fmt.Sprintf("Saved %s on #%02d", fileName, num))
	if changed {
		return sseToast(c, "ok", fmt.Sprintf("Saved %s — committed to git. Restarts apply.", fileName), map[string]any{
			"showEditor": false,
		})
	}
	return sseToast(c, "ok", fmt.Sprintf("%s is unchanged.", fileName), map[string]any{
		"showEditor": false,
	})
}

func (s *FiberServer) mcInstanceConfigDelete(c *fiber.Ctx) error {
	if s.mcInstances == nil || s.git == nil {
		return sseToast(c, "err", "Instance manager or Git committer unconfigured.", nil)
	}
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return sseToast(c, "err", "Invalid instance number.", nil)
	}
	inst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return sseToast(c, "err", "Instance not found.", nil)
	}

	var req struct {
		File string `json:"file" form:"file"`
	}
	_ = c.BodyParser(&req)

	fileName := strings.TrimSpace(req.File)
	if fileName == "" {
		fileName = strings.TrimSpace(c.FormValue("file"))
	}
	if fileName == "" {
		return sseToast(c, "err", "File name required.", nil)
	}

	relPath := fmt.Sprintf("manifests/minecraft-modded/instance-%02d/configs.yaml", inst.Number)
	changed, err := s.git.DeleteData(c.UserContext(), relPath, fileName,
		fmt.Sprintf("agrelha: delete config %s for instance #%02d", fileName, inst.Number))
	if err != nil {
		return sseToast(c, "err", "Delete failed: "+err.Error(), nil)
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-config-delete", fmt.Sprintf("Deleted %s on #%02d", fileName, num))
	if changed {
		return sseToast(c, "ok", fmt.Sprintf("Deleted %s — committed to git.", fileName), nil)
	}
	return sseToast(c, "ok", fmt.Sprintf("%s was not present.", fileName), nil)
}

// mcInstanceModsInstall adds a Modrinth mod to one Instance's mods.txt,
// resolving required dependencies first. The per-instance counterpart to
// mcInstanceModsRemove — without it, adding a mod would require recreating the
// Instance, which our own rules reserve for changing the Pack or Loader.
func (s *FiberServer) mcInstanceModsInstall(c *fiber.Ctx) error {
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
	if s.git == nil {
		return sseToast(c, "err", "GitOps plane disabled — no Codeberg token configured.", nil)
	}

	inst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return sseToast(c, "err", "Instance not found.", nil)
	}

	wanted := []string{slug}
	if s.mr != nil {
		deps, err := s.mr.ResolveRequiredDependencies(c.UserContext(), slug, inst.MCVersion, string(inst.Loader))
		if err != nil {
			slog.Warn("could not resolve all mod dependencies", "slug", slug, "instance", num, "err", err)
		}
		wanted = append(wanted, deps...)
	}

	modsPath := fmt.Sprintf("manifests/minecraft-modded/instance-%02d/mods.yaml", num)
	msg := fmt.Sprintf("mc: install %s into instance #%02d", slug, num)
	_, err = s.git.Patch(c.UserContext(), modsPath, "mods.txt", msg, func(cur string) (string, error) {
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
		return sseToast(c, "err", "Install failed: "+err.Error(), nil)
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-mod-install", fmt.Sprintf("Installed %s into #%02d", slug, num))
	return sseToast(c, "ok", fmt.Sprintf("Installed %s (+%d deps). Updating...", slug, len(wanted)-1), nil)
}
