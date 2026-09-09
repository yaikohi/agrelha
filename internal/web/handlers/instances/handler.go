package instances

import (
	"context"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/infra/content/mcversions"
	"agrelha/internal/infra/content/modpackindex"
	"agrelha/internal/infra/content/modrinth"
	"agrelha/internal/infra/gitops"
	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/store"
	"agrelha/internal/minecraft"
	"agrelha/internal/platform/config"
	"agrelha/internal/web/shared"
)

// InstanceStat holds cached per-instance stats for players, status, and uptime.
type InstanceStat struct {
	Players      int
	PlayersKnown bool
	Uptime       string
}

type instanceStatsCache struct {
	mu   sync.Mutex
	at   time.Time
	data map[int]InstanceStat
}

const instanceStatsTTL = 15 * time.Second

// Config specifies dependencies for the Minecraft instances handlers.
type Config struct {
	Cfg                     *config.Config
	Store                   *store.Store
	Git                     *gitops.Committer
	MCK8s                   *k8s.Client
	MCInstances             *minecraft.InstanceManager
	MCRconPool              *minecraft.RconPool
	MCV                     *mcversions.Client
	MPI                     *modpackindex.Client
	MR                      *modrinth.Client
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
	// Dashboard (provisioning wizard routes live in internal/http/wizard)
	router.Get("/minecraft", h.MCDashboard)

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
	router.Get("/minecraft/:num<int>/:tab", h.MCInstancePage)
	router.Post("/api/minecraft/:num<int>/settings", h.MCInstanceSettingsSave)
	router.Post("/api/minecraft/:num<int>/mods/install", h.MCInstanceModsInstall)
	router.Post("/api/minecraft/:num<int>/mods/remove", h.MCInstanceModsRemove)
	router.Get("/api/minecraft/:num<int>/configs/file", h.MCInstanceConfigGet)
	router.Post("/api/minecraft/:num<int>/configs/save", h.MCInstanceConfigSave)
	router.Post("/api/minecraft/:num<int>/configs/delete", h.MCInstanceConfigDelete)
	router.Get("/api/minecraft/:num<int>/mods/export", h.MCInstanceExport)
}

// InstanceStats returns per-instance player counts and uptimes for running instances, cached for instanceStatsTTL.
func (h *Handler) InstanceStats(ctx context.Context, insts []minecraft.Instance) map[int]InstanceStat {
	h.instStats.mu.Lock()
	defer h.instStats.mu.Unlock()

	if h.instStats.data != nil && time.Since(h.instStats.at) < instanceStatsTTL {
		return h.instStats.data
	}

	out := make(map[int]InstanceStat, len(insts))
	for _, inst := range insts {
		if inst.State != minecraft.StateRunning {
			continue
		}
		st := InstanceStat{}

		if h.cfg.MCK8s != nil {
			if ps, err := h.cfg.MCK8s.DeploymentPodStatus(ctx, inst.DeploymentName()); err == nil && !ps.StartedAt.IsZero() {
				st.Uptime = shared.HumanDuration(time.Since(ps.StartedAt))
			}
		}
		if h.cfg.MCRconPool != nil && inst.LBIP != "" {
			if res, err := h.cfg.MCRconPool.ClientFor(inst.LBIP + ":25575").Execute("/list"); err == nil {
				st.Players = len(minecraft.ParsePlayerList(res))
				st.PlayersKnown = true
			}
		}
		out[inst.Number] = st
	}

	h.instStats.data = out
	h.instStats.at = time.Now()
	return out
}
