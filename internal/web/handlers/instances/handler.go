package instances

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

// InstanceStat holds cached per-instance stats for players, status, and uptime.
type InstanceStat = instances.InstanceStat

type instanceStatsCache struct {
	mu   sync.Mutex
	at   time.Time
	data map[int]InstanceStat
}

const instanceStatsTTL = 15 * time.Second

// Config specifies dependencies for the Minecraft instances handlers.
type Config struct {
	MCInstances             *instances.InstanceManager
	MinecraftGame           ports.Game
	SearchMods              SearchModsFunc
	Actor                   func(*fiber.Ctx) string
	ApplyMinecraftAfterSync func(cmName, depName, key string, want func(string) bool)
}

// Handler provides HTTP endpoints for Minecraft instances lifecycle, wizard, and configs.
type Handler struct {
	cfg       Config
	instStats instanceStatsCache
}

// New creates a new instances Handler.
func New(cfg Config) *Handler {
	if cfg.Actor == nil {
		cfg.Actor = func(c *fiber.Ctx) string {
			if a, ok := c.Locals("actor").(string); ok && a != "" {
				return a
			}
			return "local"
		}
	}
	if cfg.ApplyMinecraftAfterSync == nil {
		cfg.ApplyMinecraftAfterSync = func(string, string, string, func(string) bool) {}
	}
	return &Handler{cfg: cfg}
}

// Register mounts all instance management routes onto the Fiber router.
func (h *Handler) Register(router fiber.Router) {
	h.RegisterPublic(router)
	h.RegisterProtected(router)
}

// RegisterPublic mounts unauthenticated routes (e.g. modpack export).
func (h *Handler) RegisterPublic(router fiber.Router) {
	router.Get("/api/minecraft/:num<int>/mods/export", h.MCInstanceExport)
}

// RegisterProtected mounts authenticated instance management routes.
func (h *Handler) RegisterProtected(router fiber.Router) {
	// Dashboard (provisioning wizard routes live in internal/http/wizard)
	router.Get("/minecraft", h.MCDashboard)
	router.Get("/minecraft/mods", h.LegacyModsRedirect)
	router.Get("/minecraft/configs", h.LegacyConfigsRedirect)

	// Direct Instance lifecycle
	router.Post("/api/minecraft/instances", h.MCInstanceCreate)
	router.Post("/api/minecraft/instances/:num/start", h.MCInstanceStart)
	router.Post("/api/minecraft/instances/:num/stop", h.MCInstanceStop)
	router.Post("/api/minecraft/instances/:num/restart", h.MCInstanceRestart)
	router.Delete("/api/minecraft/instances/:num", h.MCInstanceDelete)

	// Configs (global / default)
	router.Get("/minecraft/configs/new", h.MCConfigNew)
	router.Get("/minecraft/configs/edit", h.MCConfigEdit)
	router.Post("/minecraft/configs/save", h.MCConfigSave)
	router.Post("/minecraft/configs/delete", h.MCConfigDelete)

	// Per-instance detail & controls
	router.Get("/minecraft/:num<int>", func(c *fiber.Ctx) error {
		return c.Redirect(fmt.Sprintf("/minecraft/%s/overview", c.Params("num")))
	})
	router.Get("/minecraft/:num<int>/:tab", h.MCInstancePage)
	router.Post("/api/minecraft/:num<int>/settings", h.MCInstanceSettingsSave)
	router.Get("/api/minecraft/:num<int>/mods/search", h.MCInstanceModsSearch)
	router.Post("/api/minecraft/:num<int>/mods/search", h.MCInstanceModsSearch)
	router.Post("/api/minecraft/:num<int>/mods/install", h.MCInstanceModsInstall)
	router.Post("/api/minecraft/:num<int>/mods/remove", h.MCInstanceModsRemove)
	router.Get("/api/minecraft/:num<int>/configs/file", h.MCInstanceConfigGet)
	router.Post("/api/minecraft/:num<int>/configs/save", h.MCInstanceConfigSave)
	router.Post("/api/minecraft/:num<int>/configs/delete", h.MCInstanceConfigDelete)
}

func (h *Handler) LegacyModsRedirect(c *fiber.Ctx) error {
	if h.cfg.MCInstances != nil {
		if insts, err := h.cfg.MCInstances.ListInstances(c.UserContext()); err == nil && len(insts) > 0 {
			return c.Redirect(fmt.Sprintf("/minecraft/%d/mods", insts[0].Number), fiber.StatusTemporaryRedirect)
		}
	}
	return c.Redirect("/minecraft", fiber.StatusTemporaryRedirect)
}

func (h *Handler) LegacyConfigsRedirect(c *fiber.Ctx) error {
	if h.cfg.MCInstances != nil {
		if insts, err := h.cfg.MCInstances.ListInstances(c.UserContext()); err == nil && len(insts) > 0 {
			return c.Redirect(fmt.Sprintf("/minecraft/%d/configs", insts[0].Number), fiber.StatusTemporaryRedirect)
		}
	}
	return c.Redirect("/minecraft", fiber.StatusTemporaryRedirect)
}

// InstanceStats returns per-instance player counts and uptimes for running instances, cached for instanceStatsTTL.
func (h *Handler) InstanceStats(ctx context.Context, insts []domain.Instance) map[int]InstanceStat {
	h.instStats.mu.Lock()
	defer h.instStats.mu.Unlock()

	if h.instStats.data != nil && time.Since(h.instStats.at) < instanceStatsTTL {
		return h.instStats.data
	}

	if h.cfg.MCInstances == nil {
		return nil
	}

	out := h.cfg.MCInstances.InstanceStats(ctx, insts)
	h.instStats.data = out
	h.instStats.at = time.Now()
	return out
}
