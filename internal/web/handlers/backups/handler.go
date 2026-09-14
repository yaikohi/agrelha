package backups

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"agrelha/internal/web/shared"

	"github.com/gofiber/fiber/v2"
)

// Config holds dependencies for backup HTTP handlers.
type Config struct {
	BackupsDir       string
	MCInstances      *instances.InstanceManager
	ValheimInstances *instances.InstanceManager
	Actor            func(*fiber.Ctx) string
}

// Handler manages backup and restore HTTP endpoints.
type Handler struct {
	cfg Config
}

var readDir = os.ReadDir

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

	router.Post("/api/valheim/:num<int>/backups/create", h.ValheimCreate)
	router.Post("/api/valheim/:num<int>/backups/restore-inplace", h.ValheimRestoreInPlace)
	router.Post("/api/valheim/:num<int>/backups/restore-new", h.ValheimRestoreNew)
	router.Get("/api/valheim/:num<int>/backups/download", h.Download)
	router.Post("/api/valheim/:num<int>/backups/delete", h.ValheimDelete)
}

// BackupInfo returns cached backup stats for the dashboard tile.
func (h *Handler) BackupInfo() (domain.BackupSummary, bool) {
	if h.cfg.MCInstances != nil {
		return h.cfg.MCInstances.BackupSummary()
	}
	if h.cfg.BackupsDir == "" {
		return domain.BackupSummary{}, false
	}
	entries, err := readDir(h.cfg.BackupsDir)
	if err != nil {
		return domain.BackupSummary{}, false
	}
	var sum domain.BackupSummary
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		sum.Count++
		sum.TotalSize += fi.Size()
		if fi.ModTime().After(sum.LatestAt) {
			sum.LatestAt = fi.ModTime()
			sum.LatestName = e.Name()
			sum.LatestSize = fi.Size()
		}
	}
	return sum, true
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

	backupName, err := h.cfg.MCInstances.CreateBackup(c.UserContext(), num, h.cfg.Actor(c))
	if err != nil {
		return shared.SSEToast(c, "err", err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Backup job started: %s. Archiving to NAS...", backupName), nil)
}

// RestoreInPlace uncompresses an archive back into an existing stopped instance PVC.
func (h *Handler) RestoreInPlace(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return shared.SSEToast(c, "err", "Instance manager unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	var req struct {
		Archive string `json:"archive" form:"archive"`
	}
	_ = c.BodyParser(&req)

	archive := strings.TrimSpace(req.Archive)
	if archive == "" {
		archive = strings.TrimSpace(c.FormValue("archive"))
	}
	if archive == "" {
		archive = strings.TrimSpace(c.Query("archive"))
	}

	if err := h.cfg.MCInstances.RestoreInPlace(c.UserContext(), num, archive, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", err.Error(), nil)
	}

	archiveName := filepath.Base(archive)
	return shared.SSEToast(c, "ok", fmt.Sprintf("In-place restore started from %s (safety snapshot saved). World data is unpacking.", archiveName), nil)
}

// RestoreNew provisions a new instance initialized from an existing backup archive.
func (h *Handler) RestoreNew(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return shared.SSEToast(c, "err", "Instance manager unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	var req struct {
		Name    string `json:"name" form:"name"`
		Tier    string `json:"tier" form:"tier"`
		Archive string `json:"archive" form:"archive"`
	}
	_ = c.BodyParser(&req)

	created, err := h.cfg.MCInstances.RestoreNew(c.UserContext(), num, req.Name, req.Tier, req.Archive, h.cfg.Actor(c))
	if err != nil {
		return shared.SSEToast(c, "err", err.Error(), nil)
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
	var req struct {
		File string `json:"file" form:"file"`
	}
	_ = c.BodyParser(&req)

	file := strings.TrimSpace(req.File)
	if file == "" {
		file = strings.TrimSpace(c.FormValue("file"))
	}
	if file == "" {
		file = strings.TrimSpace(c.Query("file"))
	}
	fileName := filepath.Base(file)
	if fileName == "" || fileName == "." {
		return shared.SSEToast(c, "err", "File name required.", nil)
	}

	num, _ := strconv.Atoi(c.Params("num"))
	if h.cfg.MCInstances != nil {
		if err := h.cfg.MCInstances.DeleteBackup(c.UserContext(), num, fileName, h.cfg.Actor(c)); err != nil {
			return shared.SSEToast(c, "err", err.Error(), nil)
		}
	} else if h.cfg.BackupsDir != "" {
		if !domain.IsSafeBackupFileName(fileName) {
			return shared.SSEToast(c, "err", "Failed to delete backup: invalid backup file name", nil)
		}
		if err := os.Remove(filepath.Join(h.cfg.BackupsDir, fileName)); err != nil {
			return shared.SSEToast(c, "err", "Failed to delete backup: "+err.Error(), nil)
		}
	} else {
		return shared.SSEToast(c, "err", "Backups directory unconfigured.", nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Deleted %s.", fileName), map[string]any{
		"redirect": fmt.Sprintf("/minecraft/%d/backups", num),
	})
}

// ValheimCreate launches an asynchronous backup job for a specific Valheim instance.
func (h *Handler) ValheimCreate(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	backupName, err := h.cfg.ValheimInstances.CreateBackup(c.UserContext(), num, h.cfg.Actor(c))
	if err != nil {
		return shared.SSEToast(c, "err", err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Backup job started: %s. Archiving to NAS...", backupName), nil)
}

// ValheimRestoreInPlace uncompresses an archive back into an existing stopped Valheim instance PVC.
func (h *Handler) ValheimRestoreInPlace(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	var req struct {
		Archive string `json:"archive" form:"archive"`
	}
	_ = c.BodyParser(&req)

	archive := strings.TrimSpace(req.Archive)
	if archive == "" {
		archive = strings.TrimSpace(c.FormValue("archive"))
	}
	if archive == "" {
		archive = strings.TrimSpace(c.Query("archive"))
	}

	if err := h.cfg.ValheimInstances.RestoreInPlace(c.UserContext(), num, archive, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", err.Error(), nil)
	}

	archiveName := filepath.Base(archive)
	return shared.SSEToast(c, "ok", fmt.Sprintf("In-place restore started from %s (safety snapshot saved). World data is unpacking.", archiveName), nil)
}

// ValheimRestoreNew provisions a new Valheim instance initialized from an existing backup archive.
func (h *Handler) ValheimRestoreNew(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	var req struct {
		Name    string `json:"name" form:"name"`
		Tier    string `json:"tier" form:"tier"`
		Archive string `json:"archive" form:"archive"`
	}
	_ = c.BodyParser(&req)

	created, err := h.cfg.ValheimInstances.RestoreNew(c.UserContext(), num, req.Name, req.Tier, req.Archive, h.cfg.Actor(c))
	if err != nil {
		return shared.SSEToast(c, "err", err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Valheim server %q created from backup! Redirecting...", created.Name), map[string]any{
		"redirect": fmt.Sprintf("/valheim/%d/overview", created.Number),
	})
}

// ValheimDelete removes a Valheim backup archive from disk.
func (h *Handler) ValheimDelete(c *fiber.Ctx) error {
	var req struct {
		File string `json:"file" form:"file"`
	}
	_ = c.BodyParser(&req)

	file := strings.TrimSpace(req.File)
	if file == "" {
		file = strings.TrimSpace(c.FormValue("file"))
	}
	if file == "" {
		file = strings.TrimSpace(c.Query("file"))
	}
	fileName := filepath.Base(file)
	if fileName == "" || fileName == "." {
		return shared.SSEToast(c, "err", "File name required.", nil)
	}

	num, _ := strconv.Atoi(c.Params("num"))
	if h.cfg.ValheimInstances != nil {
		if err := h.cfg.ValheimInstances.DeleteBackup(c.UserContext(), num, fileName, h.cfg.Actor(c)); err != nil {
			return shared.SSEToast(c, "err", err.Error(), nil)
		}
	} else if h.cfg.BackupsDir != "" {
		if !domain.IsSafeBackupFileName(fileName) {
			return shared.SSEToast(c, "err", "Failed to delete backup: invalid backup file name", nil)
		}
		if err := os.Remove(filepath.Join(h.cfg.BackupsDir, fileName)); err != nil {
			return shared.SSEToast(c, "err", "Failed to delete backup: "+err.Error(), nil)
		}
	} else {
		return shared.SSEToast(c, "err", "Backups directory unconfigured.", nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Deleted %s.", fileName), map[string]any{
		"redirect": fmt.Sprintf("/valheim/%d/backups", num),
	})
}
