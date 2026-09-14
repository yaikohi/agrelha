package valheim

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/instances"
	"agrelha/internal/app/modupdates"
	"agrelha/internal/domain"
	"mime/multipart"

	"agrelha/internal/ports"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/sse"
)

var (
	innerElement      = sse.InnerElement
	patchSignals      = sse.PatchSignals
	renderUpdatePanel = func(ctx context.Context, d pages.InstanceDetailUI, w io.Writer) error {
		return pages.ModUpdatePanel(d).Render(ctx, w)
	}
	openFormFile = func(fh *multipart.FileHeader) (multipart.File, error) { return fh.Open() }
	readFormFile = io.ReadAll
)

// InstanceStat holds cached per-instance stats for players, status, and uptime.
type InstanceStat = instances.InstanceStat

type instanceStatsCache struct {
	mu   sync.Mutex
	at   time.Time
	data map[int]InstanceStat
}

const instanceStatsTTL = 15 * time.Second

// Config specifies dependencies for the Valheim instances handlers.
type Config struct {
	LastIncident          func(ctx context.Context, number int) (*domain.Incident, error)
	ValheimInstances      *instances.InstanceManager
	ValheimGame           ports.Game
	ModUpdates            *modupdates.Checker
	TS                    ports.PackageCatalog
	ReadmeCache           ports.ReadmeCache
	Actor                 func(*fiber.Ctx) string
	ApplyValheimAfterSync func(cmName, depName, key string, want func(string) bool)
	LegacyConsole         func(*fiber.Ctx) error
}

// Handler provides HTTP endpoints for Valheim instances lifecycle, wizard, and configs.
type Handler struct {
	cfg       Config
	instStats instanceStatsCache
}

// New creates a new Valheim Handler.
func New(cfg Config) *Handler {
	if cfg.Actor == nil {
		cfg.Actor = func(c *fiber.Ctx) string {
			if a, ok := c.Locals("actor").(string); ok && a != "" {
				return a
			}
			return "local"
		}
	}
	if cfg.ApplyValheimAfterSync == nil {
		cfg.ApplyValheimAfterSync = func(string, string, string, func(string) bool) {}
	}
	return &Handler{cfg: cfg}
}

// Register mounts all Valheim instance, wizard, and content routes.
func (h *Handler) Register(router fiber.Router) {
	h.RegisterPublic(router)
	h.RegisterProtected(router)
}

// RegisterPublic mounts unauthenticated routes (e.g. modpack export).
func (h *Handler) RegisterPublic(router fiber.Router) {
	router.Get("/api/valheim/:num<int>/mods/export", h.ValheimInstanceExport)
	router.Get("/valheim/mods/export", h.LegacyExportRedirect)
	router.Get("/mods/export", h.LegacyExportRedirect)
}

// RegisterProtected mounts authenticated Valheim routes.
func (h *Handler) RegisterProtected(router fiber.Router) {
	router.Get("/valheim", h.ValheimDashboard)
	router.Get("/valheim/mods", h.LegacyModsRedirect)
	router.Get("/valheim/configs", h.LegacyConfigsRedirect)
	router.Get("/valheim/access", func(c *fiber.Ctx) error {
		return c.Redirect("/admins", fiber.StatusTemporaryRedirect)
	})

	router.Get("/mods", h.LegacyModsRedirect)
	router.Get("/configs", h.LegacyConfigsRedirect)

	// Direct Instance lifecycle
	router.Post("/api/valheim/instances/:num/start", h.ValheimInstanceStart)
	router.Post("/api/valheim/instances/:num/stop", h.ValheimInstanceStop)
	router.Post("/api/valheim/instances/:num/restart", h.ValheimInstanceRestart)
	router.Delete("/api/valheim/instances/:num", h.ValheimInstanceDelete)

	// Wizard
	router.Get("/valheim/create", h.ValheimWizardPage)
	router.Post("/api/valheim/wizard/create", h.ValheimWizardCreate)
	router.Post("/api/valheim/wizard/import", h.ValheimWizardImport)
	router.Get("/api/valheim/wizard/mods/search", h.ValheimWizardModsSearch)
	router.Post("/api/valheim/wizard/mods/search", h.ValheimWizardModsSearch)
	router.Get("/api/valheim/wizard/mods/detail", h.ValheimWizardModDetail)
	router.Post("/api/valheim/wizard/mods/detail", h.ValheimWizardModDetail)
	router.Post("/api/valheim/wizard/cart/sync", h.ValheimWizardCartSync)

	// Per-instance detail & controls
	router.Get("/valheim/:num<int>", func(c *fiber.Ctx) error {
		return c.Redirect(fmt.Sprintf("/valheim/%s/overview", c.Params("num")))
	})
	router.Get("/valheim/:num<int>/:tab", h.ValheimInstancePage)
	router.Post("/api/valheim/:num<int>/settings", h.ValheimInstanceSettingsSave)
	router.Get("/api/valheim/:num<int>/mods/search", h.ValheimInstanceModsSearch)
	router.Post("/api/valheim/:num<int>/mods/search", h.ValheimInstanceModsSearch)
	router.Get("/api/valheim/:num<int>/mods/detail", h.ValheimModDetail)
	router.Post("/api/valheim/:num<int>/mods/detail", h.ValheimModDetail)
	router.Post("/api/valheim/:num<int>/mods/install", h.ValheimInstanceModsInstall)
	router.Post("/api/valheim/:num<int>/mods/remove", h.ValheimInstanceModsRemove)
	router.Post("/api/valheim/:num<int>/mods/updates/check", h.ValheimModUpdatesCheck)
	router.Post("/api/valheim/:num<int>/mods/updates/apply", h.ValheimModUpdatesApply)
	router.Post("/api/valheim/:num<int>/mods/updates/undo", h.ValheimModUpdatesUndo)
	router.Get("/api/valheim/:num<int>/configs/file", h.ValheimInstanceConfigGet)
	router.Post("/api/valheim/:num<int>/configs/save", h.ValheimInstanceConfigSave)
	router.Post("/api/valheim/:num<int>/configs/delete", h.ValheimInstanceConfigDelete)
}

func (h *Handler) LegacyModsRedirect(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances != nil {
		if insts, err := h.cfg.ValheimInstances.ListInstances(c.UserContext()); err == nil && len(insts) > 0 {
			return c.Redirect(fmt.Sprintf("/valheim/%d/mods", insts[0].Number), fiber.StatusTemporaryRedirect)
		}
	}
	return c.Redirect("/valheim", fiber.StatusTemporaryRedirect)
}

func (h *Handler) LegacyConfigsRedirect(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances != nil {
		if insts, err := h.cfg.ValheimInstances.ListInstances(c.UserContext()); err == nil && len(insts) > 0 {
			return c.Redirect(fmt.Sprintf("/valheim/%d/configs", insts[0].Number), fiber.StatusTemporaryRedirect)
		}
	}
	return c.Redirect("/valheim", fiber.StatusTemporaryRedirect)
}

func (h *Handler) LegacyExportRedirect(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances != nil {
		if insts, err := h.cfg.ValheimInstances.ListInstances(c.UserContext()); err == nil && len(insts) > 0 {
			return c.Redirect(fmt.Sprintf("/api/valheim/%d/mods/export", insts[0].Number), fiber.StatusTemporaryRedirect)
		}
	}
	return c.Redirect("/valheim", fiber.StatusTemporaryRedirect)
}

// InstanceStats returns per-instance player counts and uptimes for running instances, cached for instanceStatsTTL.
func (h *Handler) InstanceStats(ctx context.Context, insts []domain.Instance) map[int]InstanceStat {
	h.instStats.mu.Lock()
	defer h.instStats.mu.Unlock()

	if h.instStats.data != nil && time.Since(h.instStats.at) < instanceStatsTTL {
		return h.instStats.data
	}

	if h.cfg.ValheimInstances == nil {
		return nil
	}

	out := h.cfg.ValheimInstances.InstanceStats(ctx, insts)
	h.instStats.data = out
	h.instStats.at = time.Now()
	return out
}
