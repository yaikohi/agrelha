package backups

import (
	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"agrelha/internal/infra/rcon"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"agrelha/internal/infra/backups"
	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/store"
	"agrelha/internal/web/shared"

	"github.com/gofiber/fiber/v2"
)

// Config holds dependencies for backup HTTP handlers.
type Config struct {
	BackupsDir  string
	MCInstances *instances.InstanceManager
	MCK8s       *k8s.Client
	RconPool    *rcon.Pool
	Store       *store.Store
	Actor       func(*fiber.Ctx) string
}

// Handler manages backup and restore HTTP endpoints.
type Handler struct {
	cfg Config

	bkMu   sync.Mutex
	bkInfo backups.Info
	bkOK   bool
	bkAt   time.Time
}

// New creates a new backup Handler.
func New(cfg Config) *Handler {
	if cfg.Actor == nil {
		cfg.Actor = func(c *fiber.Ctx) string {
			if a, ok := c.Locals("actor").(string); ok && a != "" {
				return a
			}
			return "-"
		}
	}
	return &Handler{cfg: cfg}
}

// Register mounts all backup routes onto the provided Fiber router.
func (h *Handler) Register(router fiber.Router) {
	router.Post("/api/minecraft/:num<int>/backups/create", h.Create)
	router.Post("/api/minecraft/:num<int>/backups/restore-inplace", h.RestoreInPlace)
	router.Post("/api/minecraft/:num<int>/backups/restore-new", h.RestoreNew)
	router.Get("/api/minecraft/:num<int>/backups/download", h.Download)
	router.Post("/api/minecraft/:num<int>/backups/delete", h.Delete)
}

// BackupInfo returns cached backup stats for the dashboard tile, refreshing at most once per minute.
func (h *Handler) BackupInfo() (backups.Info, bool) {
	h.bkMu.Lock()
	if !h.bkAt.IsZero() && time.Since(h.bkAt) < time.Minute {
		i, ok := h.bkInfo, h.bkOK
		h.bkMu.Unlock()
		return i, ok
	}
	h.bkAt = time.Now()
	h.bkMu.Unlock()

	type res struct {
		i  backups.Info
		ok bool
	}
	ch := make(chan res, 1)
	go func() {
		i, err := backups.Stat(h.cfg.BackupsDir)
		ch <- res{i, err == nil}
	}()
	var out res
	select {
	case out = <-ch:
	case <-time.After(3 * time.Second):
	}

	h.bkMu.Lock()
	h.bkInfo, h.bkOK = out.i, out.ok
	h.bkMu.Unlock()
	return out.i, out.ok
}

// Create launches an asynchronous backup job for a specific instance.
func (h *Handler) Create(c *fiber.Ctx) error {
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

	// Flush world save via RCON if running
	if inst.State == domain.StateRunning && h.cfg.RconPool != nil {
		addr := fmt.Sprintf("%s.minecraft-modded.svc.cluster.local:25575", inst.ServiceName())
		client := h.cfg.RconPool.ClientFor(addr)
		_, _ = client.Execute("/save-off")
		_, _ = client.Execute("/save-all flush")
		defer func() {
			_, _ = client.Execute("/save-on")
		}()
	}

	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("mc-%s-%02d-%s.tar.gz", inst.Slug, inst.Number, timestamp)
	jobName := fmt.Sprintf("mc-bkp-%s-%d-%s", inst.Slug, inst.Number, time.Now().Format("150405"))

	if h.cfg.MCK8s != nil {
		dataPVC := fmt.Sprintf("mc-instance-%02d-data", inst.Number)
		backupsPVC := "minecraft-modded-backups"
		if err := h.cfg.MCK8s.CreateBackupJob(c.UserContext(), jobName, backupName, dataPVC, backupsPVC); err != nil {
			return shared.SSEToast(c, "err", fmt.Sprintf("Failed to launch backup Job: %s", err.Error()), nil)
		}
	}

	if h.cfg.BackupsDir != "" {
		_ = backups.PruneBackups(h.cfg.BackupsDir, inst.Slug, inst.Number, 5)
	}

	if h.cfg.Store != nil {
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "mc-backup", fmt.Sprintf("Backup %s for instance #%02d", backupName, num))
		_ = h.cfg.Store.RecordEvent("mc-backup", h.cfg.Actor(c))
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Backup job started: %s. Archiving to NAS...", backupName), nil)
}

// RestoreInPlace uncompresses an archive back into an existing stopped instance PVC.
func (h *Handler) RestoreInPlace(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil || h.cfg.MCK8s == nil {
		return shared.SSEToast(c, "err", "Instance manager or cluster client unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	inst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return shared.SSEToast(c, "err", "Instance not found.", nil)
	}

	if inst.State == domain.StateRunning {
		return shared.SSEToast(c, "err", "Cannot restore while world is running. Please stop the server first.", nil)
	}

	var req struct {
		Archive string `json:"archive" form:"archive"`
	}
	_ = c.BodyParser(&req)
	archiveName := filepath.Base(strings.TrimSpace(req.Archive))
	if archiveName == "" || archiveName == "." || !strings.HasSuffix(archiveName, ".tar.gz") {
		return shared.SSEToast(c, "err", "Valid backup archive name required.", nil)
	}

	// 1. Take pre-restore safety snapshot
	safetyArchive := backups.FormatBackupFileName(inst.Slug, inst.Number, "prerestore")
	safetyJob := fmt.Sprintf("mc-bkp-%s-%d-%s", inst.Slug, inst.Number, time.Now().Format("150405"))
	_ = h.cfg.MCK8s.CreateBackupJob(c.UserContext(), safetyJob, safetyArchive, inst.PVCName(), "minecraft-modded-backups")

	// 2. Launch restore job
	restoreJobName := fmt.Sprintf("mc-rst-%s-%d-%s", inst.Slug, inst.Number, time.Now().Format("150405"))
	if err := h.cfg.MCK8s.CreateRestoreJob(c.UserContext(), restoreJobName, archiveName, inst.PVCName(), "minecraft-modded-backups"); err != nil {
		return shared.SSEToast(c, "err", "Failed to launch restore Job: "+err.Error(), nil)
	}

	if h.cfg.Store != nil {
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "mc-backup-restore-inplace", fmt.Sprintf("Restored %s into #%02d", archiveName, num))
		_ = h.cfg.Store.RecordEvent("mc-backup-restore-inplace", h.cfg.Actor(c))
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("In-place restore started from %s (safety snapshot saved). World data is unpacking.", archiveName), nil)
}

// RestoreNew provisions a new instance initialized from an existing backup archive.
func (h *Handler) RestoreNew(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil || h.cfg.MCK8s == nil {
		return shared.SSEToast(c, "err", "Instance manager or cluster client unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	srcInst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), num)
	if err != nil || srcInst == nil {
		return shared.SSEToast(c, "err", "Source instance not found.", nil)
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
		return shared.SSEToast(c, "err", "Valid backup archive name required.", nil)
	}

	newTier := srcInst.Tier
	if req.Tier != "" {
		newTier = domain.NormalizeTier(req.Tier)
	}

	newInst := domain.Instance{
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
		State:      domain.StateStopped,
	}

	modsTxt := ""
	if data, err := h.cfg.MCK8s.ConfigMapData(c.UserContext(), srcInst.ModsCMName()); err == nil {
		modsTxt = data["mods.txt"]
	}

	created, err := h.cfg.MCInstances.CreateInstance(c.UserContext(), newInst, modsTxt)
	if err != nil {
		return shared.SSEToast(c, "err", "Failed to create new instance: "+err.Error(), nil)
	}

	restoreJobName := fmt.Sprintf("mc-rst-%s-%d-%s", created.Slug, created.Number, time.Now().Format("150405"))
	if err := h.cfg.MCK8s.CreateRestoreJob(c.UserContext(), restoreJobName, archiveName, created.PVCName(), "minecraft-modded-backups"); err != nil {
		return shared.SSEToast(c, "err", "Instance created, but restore Job failed: "+err.Error(), nil)
	}

	if h.cfg.Store != nil {
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "mc-backup-restore-new", fmt.Sprintf("Restored %s into new instance #%02d %q", archiveName, created.Number, created.Name))
		_ = h.cfg.Store.RecordEvent("mc-backup-restore-new", h.cfg.Actor(c))
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("World %q created from backup! Redirecting...", created.Name), map[string]any{
		"redirect": fmt.Sprintf("/minecraft/provisioning/%d", created.Number),
	})
}

// Download serves the tar.gz backup file for download.
func (h *Handler) Download(c *fiber.Ctx) error {
	if h.cfg.BackupsDir == "" {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Backups directory unconfigured")
	}

	fileName := filepath.Base(strings.TrimSpace(c.Query("f")))
	if fileName == "" || fileName == "." || !strings.HasSuffix(fileName, ".tar.gz") {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid backup file")
	}

	filePath := filepath.Join(h.cfg.BackupsDir, fileName)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return c.Status(fiber.StatusNotFound).SendString("Backup file not found")
	}

	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	return c.SendFile(filePath)
}

// Delete removes a backup archive from disk.
func (h *Handler) Delete(c *fiber.Ctx) error {
	if h.cfg.BackupsDir == "" {
		return shared.SSEToast(c, "err", "Backups directory unconfigured.", nil)
	}

	var req struct {
		File string `json:"file" form:"file"`
	}
	_ = c.BodyParser(&req)

	fileName := filepath.Base(strings.TrimSpace(req.File))
	if fileName == "" || fileName == "." {
		return shared.SSEToast(c, "err", "File name required.", nil)
	}

	if err := backups.DeleteBackup(h.cfg.BackupsDir, fileName); err != nil {
		return shared.SSEToast(c, "err", "Failed to delete backup: "+err.Error(), nil)
	}

	num, _ := strconv.Atoi(c.Params("num"))
	if h.cfg.Store != nil {
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "mc-backup-delete", fmt.Sprintf("Deleted backup %s for #%02d", fileName, num))
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Deleted %s.", fileName), map[string]any{
		"redirect": fmt.Sprintf("/minecraft/%d/backups", num),
	})
}
