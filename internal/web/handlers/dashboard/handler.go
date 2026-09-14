package dashboard

import (
	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"time"

	"agrelha/internal/ports"
	"agrelha/internal/web/metrics"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
	"agrelha/internal/web/sse"

	"github.com/gofiber/fiber/v2"
)

// InstanceStat holds cached per-instance stats for the dashboard.
type InstanceStat struct {
	Players      int
	PlayersKnown bool
	Uptime       string
}

// BackupSummary aggregates metadata for storage and backup health reporting on the dashboard.
type BackupSummary struct {
	Count      int
	TotalSize  int64
	LatestSize int64
	LatestAt   time.Time
}

// Config defines dependencies for the dashboard page and main SSE loop.
type Config struct {
	GrafanaDashboardURL  string
	ValheimAddress       string
	GameNodeName         string
	ModUpdateTotal       func() int
	ValheimInstances     *instances.InstanceManager
	MCInstances          *instances.InstanceManager
	ValheimGame          ports.Game
	MinecraftGame        ports.Game
	Auth                 ports.Auth
	Actor                func(*fiber.Ctx) string
	BackupInfo           func() (BackupSummary, bool)
	InstanceStats        func(context.Context, []domain.Instance) map[int]InstanceStat
	ValheimInstanceStats func(context.Context, []domain.Instance) map[int]InstanceStat
	SSEInterval          time.Duration
}

// Handler serves the dashboard landing page and the continuous tile SSE stream.
type Handler struct {
	cfg Config
}

// New creates a new dashboard Handler.
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

// Register mounts dashboard routes onto the provided Fiber router.
func (h *Handler) Register(router fiber.Router) {
	router.Get("/", h.DashboardPage)
	router.Get("/sse", h.SSEMain)
}

// DashboardPage renders the public/admin landing view.
func (h *Handler) DashboardPage(c *fiber.Ctx) error {
	fk, fm := shared.TakeFlash(c)
	isAdmin := shared.IsAdmin(h.cfg.Auth, c)
	mcSummary := pages.MinecraftSummaryUI{
		MaxInstances:   4,
		MaxRunning:     2,
		TotalBudgetGiB: 24,
	}

	if h.cfg.MCInstances != nil {
		if insts, err := h.cfg.MCInstances.ListInstances(c.UserContext()); err == nil {
			var stats map[int]InstanceStat
			if h.cfg.InstanceStats != nil {
				stats = h.cfg.InstanceStats(c.UserContext(), insts)
			}
			budget := h.cfg.MCInstances.Budget(insts)
			mcSummary.TotalInstances = budget.TotalInstances
			mcSummary.RunningCount = budget.RunningCount
			mcSummary.MaxInstances = budget.MaxInstances
			mcSummary.MaxRunning = budget.MaxRunning
			mcSummary.UsedGiB = budget.UsedGiB
			mcSummary.TotalBudgetGiB = budget.TotalBudgetGiB

			for _, inst := range insts {
				if inst.State == domain.StateRunning {
					uinst := pages.InstanceUI{
						Number:    inst.Number,
						Name:      inst.Name,
						Slug:      inst.Slug,
						Loader:    string(inst.Loader),
						Source:    string(inst.Source),
						MCVersion: inst.MCVersion,
						HasMods:   hasMods(c.UserContext(), h.cfg.MCInstances, inst.Number),
						Tier:      string(inst.Tier),
						MemoryGiB: inst.MemoryGiB(),
						State:     string(inst.State),
						LBIP:      inst.LBIP,
					}
					if stats != nil {
						if st, ok := stats[inst.Number]; ok {
							uinst.Players = st.Players
							uinst.PlayersKnown = st.PlayersKnown
							uinst.Uptime = st.Uptime
						}
					}
					mcSummary.ActiveInstances = append(mcSummary.ActiveInstances, uinst)
					if mcSummary.ActiveInstance == nil {
						mcSummary.ActiveInstance = &uinst
					}
				}
			}
		}
	}

	var valheimSummary pages.ValheimSummaryUI

	if h.cfg.ValheimInstances != nil {
		valheimSummary.MaxInstances = 4
		valheimSummary.MaxRunning = 2
		valheimSummary.TotalBudgetGiB = 16
		if insts, err := h.cfg.ValheimInstances.ListInstances(c.UserContext()); err == nil {
			var stats map[int]InstanceStat
			if h.cfg.ValheimInstanceStats != nil {
				stats = h.cfg.ValheimInstanceStats(c.UserContext(), insts)
			} else if h.cfg.InstanceStats != nil {
				stats = h.cfg.InstanceStats(c.UserContext(), insts)
			}
			budget := h.cfg.ValheimInstances.Budget(insts)
			valheimSummary.TotalInstances = budget.TotalInstances
			valheimSummary.RunningCount = budget.RunningCount
			valheimSummary.MaxInstances = budget.MaxInstances
			valheimSummary.MaxRunning = budget.MaxRunning
			valheimSummary.UsedGiB = budget.UsedGiB
			valheimSummary.TotalBudgetGiB = budget.TotalBudgetGiB

			for _, inst := range insts {
				if inst.State == domain.StateRunning {
					uinst := pages.InstanceUI{
						GameID:    string(inst.GameID),
						Number:    inst.Number,
						Name:      inst.Name,
						Slug:      inst.Slug,
						Password:  inst.Password,
						Source:    string(inst.Source),
						HasMods:   hasMods(c.UserContext(), h.cfg.ValheimInstances, inst.Number),
						Seed:      inst.Seed,
						Tier:      string(inst.Tier),
						MemoryGiB: inst.MemoryGiB(),
						State:     string(inst.State),
						LBIP:      inst.LBIP,
					}
					if stats != nil {
						if st, ok := stats[inst.Number]; ok {
							uinst.Players = st.Players
							uinst.PlayersKnown = st.PlayersKnown
							uinst.Uptime = st.Uptime
						}
					}
					valheimSummary.ActiveInstances = append(valheimSummary.ActiveInstances, uinst)
					if valheimSummary.ActiveInstance == nil {
						valheimSummary.ActiveInstance = &uinst
					}
				}
			}
		}
	}

	grafanaURL := h.cfg.GrafanaDashboardURL
	valheimAddr := h.cfg.ValheimAddress
	nodeName := h.cfg.GameNodeName

	return shared.Render(c, pages.Dashboard(grafanaURL, valheimAddr, nodeName, valheimSummary, mcSummary, isAdmin, fk, fm))
}

// SSEMain streams tile signals + updates badge/list every 5s.
func (h *Handler) SSEMain(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	id := shared.RequestID(c)
	actor := h.cfg.Actor(c)
	slog.Info("sse open", "rid", id, "actor", actor, "stream", "main")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		metrics.SSEActive.Inc()
		metrics.SSEOpened.Inc()
		start := time.Now()
		var tiles int
		reason := "loop-exit"
		defer func() {
			metrics.SSEActive.Dec()
			metrics.SSEClosed.WithLabelValues(reason).Inc()
			slog.Info("sse close", "rid", id, "actor", actor, "stream", "main",
				"reason", reason, "tiles", tiles, "dur_ms", time.Since(start).Milliseconds())
		}()

		interval := h.cfg.SSEInterval
		if interval <= 0 {
			interval = 5 * time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		push := func() bool {
			if err := sse.PatchSignals(w, h.TileSignals(ctx)); err != nil {
				reason = "client-gone"
				slog.Debug("sse write failed", "rid", id, "frame", "signals", "err", err)
				return false
			}
			tiles++
			metrics.SSEFrames.WithLabelValues("signals").Inc()
			return true
		}

		if !push() {
			return
		}
		for range ticker.C {
			if !push() {
				return
			}
		}
	})
	return nil
}

// TileSignals computes the metric signals for live dashboard tiles.
func (h *Handler) TileSignals(ctx context.Context) map[string]any {
	sig := map[string]any{
		"players": "—", "cpu": "—", "mem": "—", "uptime": "—", "state": "unknown", "online": false, "backup": "—", "backupinfo": "",
		"mc_players": "—", "mc_cpu": "—", "mc_mem": "—", "mc_uptime": "—", "mc_state": "unknown", "mc_loader": "NeoForge",
		"updates": 0,
	}

	if h.cfg.ModUpdateTotal != nil {
		sig["updates"] = h.cfg.ModUpdateTotal()
	}

	if h.cfg.BackupInfo != nil {
		if bi, ok := h.cfg.BackupInfo(); ok && bi.Count > 0 {
			sig["backup"] = shared.HumanAgo(bi.LatestAt)
			sig["backupinfo"] = fmt.Sprintf("%d backups · %s total · latest %s",
				bi.Count, shared.HumanSize(bi.TotalSize), shared.HumanSize(bi.LatestSize))
		}
	}

	if h.cfg.ValheimGame != nil {
		if tele, err := h.cfg.ValheimGame.Telemetry(ctx); err == nil {
			if tele.PlayersKnown {
				sig["players"] = tele.Players
			}
			if tele.State != "" {
				sig["state"] = tele.State
			}
			sig["online"] = tele.Online
			if tele.Uptime != "" {
				sig["uptime"] = tele.Uptime
			}
			if tele.CPU != "" {
				sig["cpu"] = tele.CPU
			}
			if tele.Memory != "" {
				sig["mem"] = tele.Memory
			}
		}
	}

	if h.cfg.MinecraftGame != nil {
		if tele, err := h.cfg.MinecraftGame.Telemetry(ctx); err == nil {
			if tele.PlayersKnown {
				sig["mc_players"] = tele.Players
			}
			if tele.State != "" {
				sig["mc_state"] = tele.State
			}
			if tele.Uptime != "" {
				sig["mc_uptime"] = tele.Uptime
			}
			if tele.CPU != "" {
				sig["mc_cpu"] = tele.CPU
			}
			if tele.Memory != "" {
				sig["mc_mem"] = tele.Memory
			}
			if tele.Loader != "" {
				sig["mc_loader"] = tele.Loader
			}
			if tele.PackName != "" {
				sig["mc_pack"] = tele.PackName
			}
		}
	}

	return sig
}

// modLister is the slice of an instance manager the hub needs to tell whether a
// World has anything to export.
type modLister interface {
	GetInstalledMods(ctx context.Context, num int) ([]string, error)
}

// hasMods reports whether a World has any mods to hand a player. A Modded World
// with an empty list has nothing to download, which is separate from whether it
// runs a Loader at all.
func hasMods(ctx context.Context, m modLister, num int) bool {
	if m == nil {
		return false
	}
	mods, err := m.GetInstalledMods(ctx, num)
	return err == nil && len(mods) > 0
}
