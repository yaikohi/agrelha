package dashboard

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"time"

	"agrelha/internal/app/games"
	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
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
	GrafanaDashboardURL   string
	ValheimAddress        string
	GameNodeName          string
	Games                 *games.Registry
	InstanceManagers      map[domain.GameID]*instances.InstanceManager
	GameInstanceStats     map[domain.GameID]func(context.Context, []domain.Instance) map[int]InstanceStat
	GameModUpdateTotals   map[domain.GameID]func() int
	ModUpdateTotal        func() int
	ValheimModUpdateTotal func() int
	MCModUpdateTotal      func() int
	ValheimInstances      *instances.InstanceManager
	MCInstances           *instances.InstanceManager
	ValheimGame           ports.Game
	MinecraftGame         ports.Game
	Auth                  ports.Auth
	Actor                 func(*fiber.Ctx) string
	BackupInfo            func() (BackupSummary, bool)
	InstanceStats         func(context.Context, []domain.Instance) map[int]InstanceStat
	ValheimInstanceStats  func(context.Context, []domain.Instance) map[int]InstanceStat
	SSEInterval           time.Duration
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

func (h *Handler) registeredProfiles() []domain.GameProfile {
	if h.cfg.Games != nil {
		profs := h.cfg.Games.Profiles()
		if len(profs) > 0 {
			return profs
		}
	}
	return domain.Profiles()
}

func (h *Handler) instanceManager(id domain.GameID) *instances.InstanceManager {
	if h.cfg.InstanceManagers != nil {
		if m, ok := h.cfg.InstanceManagers[id]; ok {
			return m
		}
	}
	switch id {
	case domain.GameValheim:
		return h.cfg.ValheimInstances
	case domain.GameMinecraft:
		return h.cfg.MCInstances
	default:
		return nil
	}
}

func (h *Handler) instanceStatsFunc(id domain.GameID) func(context.Context, []domain.Instance) map[int]InstanceStat {
	if h.cfg.GameInstanceStats != nil {
		if f, ok := h.cfg.GameInstanceStats[id]; ok {
			return f
		}
	}
	switch id {
	case domain.GameValheim:
		if h.cfg.ValheimInstanceStats != nil {
			return h.cfg.ValheimInstanceStats
		}
		return h.cfg.InstanceStats
	case domain.GameMinecraft:
		return h.cfg.InstanceStats
	default:
		return h.cfg.InstanceStats
	}
}

func (h *Handler) modUpdateTotalFunc(id domain.GameID) func() int {
	if h.cfg.GameModUpdateTotals != nil {
		if f, ok := h.cfg.GameModUpdateTotals[id]; ok {
			return f
		}
	}
	switch id {
	case domain.GameValheim:
		return h.cfg.ValheimModUpdateTotal
	case domain.GameMinecraft:
		return h.cfg.MCModUpdateTotal
	default:
		return nil
	}
}

func (h *Handler) gameEngine(id domain.GameID) ports.Game {
	if h.cfg.Games != nil {
		if eng, ok := h.cfg.Games.Engine(id); ok && eng != nil {
			return eng
		}
	}
	switch id {
	case domain.GameValheim:
		return h.cfg.ValheimGame
	case domain.GameMinecraft:
		return h.cfg.MinecraftGame
	default:
		return nil
	}
}

func (h *Handler) buildSummary(ctx context.Context, p domain.GameProfile, mgr *instances.InstanceManager, statsFn func(context.Context, []domain.Instance) map[int]InstanceStat) pages.GameSummaryUI {
	var summary pages.GameSummaryUI

	if mgr == nil {
		if p.ID == domain.GameMinecraft {
			summary.MaxInstances = 4
			summary.MaxRunning = 2
			summary.TotalBudgetGiB = 24
		}
		return summary
	}

	if p.ID == domain.GameValheim {
		summary.MaxInstances = 4
		summary.MaxRunning = 2
		summary.TotalBudgetGiB = 16
	}

	insts, err := mgr.ListInstances(ctx)
	if err != nil {
		return summary
	}

	var stats map[int]InstanceStat
	if statsFn != nil {
		stats = statsFn(ctx, insts)
	}

	budget := mgr.Budget(insts)
	summary.TotalInstances = budget.TotalInstances
	summary.RunningCount = budget.RunningCount
	summary.MaxInstances = budget.MaxInstances
	summary.MaxRunning = budget.MaxRunning
	summary.UsedGiB = budget.UsedGiB
	summary.TotalBudgetGiB = budget.TotalBudgetGiB

	for _, inst := range insts {
		if inst.State != domain.StateRunning {
			continue
		}

		uinst := pages.InstanceUI{
			GameID:    string(inst.GameID),
			Number:    inst.Number,
			Name:      inst.Name,
			Slug:      inst.Slug,
			Source:    string(inst.Source),
			HasMods:   hasMods(ctx, mgr, inst.Number),
			Tier:      string(inst.Tier),
			MemoryGiB: inst.MemoryGiB(p),
			State:     string(inst.State),
			LBIP:      inst.LBIP,
		}

		if inst.Minecraft != nil {
			uinst.Loader = string(inst.Minecraft.Loader)
			uinst.MCVersion = inst.Minecraft.MCVersion
		}
		if inst.Valheim != nil {
			uinst.Password = inst.Valheim.Password
			uinst.Seed = inst.Valheim.Seed
		}

		if stats != nil {
			if st, ok := stats[inst.Number]; ok {
				uinst.Players = st.Players
				uinst.PlayersKnown = st.PlayersKnown
				uinst.Uptime = st.Uptime
			}
		}

		summary.ActiveInstances = append(summary.ActiveInstances, uinst)
		if summary.ActiveInstance == nil {
			summary.ActiveInstance = &uinst
		}
	}

	return summary
}

// DashboardPage renders the public/admin landing view.
func (h *Handler) DashboardPage(c *fiber.Ctx) error {
	fk, fm := shared.TakeFlash(c)
	isAdmin := shared.IsAdmin(c)

	var gameCards []pages.DashboardGameUI
	for _, p := range h.registeredProfiles() {
		mgr := h.instanceManager(p.ID)
		statsFn := h.instanceStatsFunc(p.ID)
		summary := h.buildSummary(c.UserContext(), p, mgr, statsFn)

		var card pages.GameCardUI
		var actions pages.CardActionsUI

		switch p.ID {
		case domain.GameValheim:
			card = pages.ValheimCard(h.cfg.ValheimAddress, h.cfg.GameNodeName, isAdmin, summary)
			actions = pages.ValheimActions(summary)
		case domain.GameMinecraft:
			card = pages.MinecraftCard(summary, isAdmin)
			actions = pages.MinecraftActions(summary)
		default:
			card = pages.GenericGameCard(p, summary, isAdmin)
			actions = pages.GameActions(p, summary)
		}

		gameCards = append(gameCards, pages.DashboardGameUI{
			Card:    card,
			Actions: actions,
		})
	}

	return shared.Render(c, pages.Dashboard(h.cfg.GrafanaDashboardURL, gameCards, isAdmin, fk, fm))
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
		"updates": 0, "valheim_updates": 0, "mc_updates": 0,
	}

	// 1. Mod update totals
	totalUpdates := 0
	hasSpecificUpdates := false
	for _, p := range h.registeredProfiles() {
		updFn := h.modUpdateTotalFunc(p.ID)
		if updFn != nil {
			cnt := updFn()
			sig[p.Prefix+"_updates"] = cnt
			totalUpdates += cnt
			hasSpecificUpdates = true
		}
	}
	if h.cfg.ModUpdateTotal != nil {
		sig["updates"] = h.cfg.ModUpdateTotal()
	} else if hasSpecificUpdates {
		sig["updates"] = totalUpdates
	}

	// 2. Backup health
	if h.cfg.BackupInfo != nil {
		if bi, ok := h.cfg.BackupInfo(); ok && bi.Count > 0 {
			sig["backup"] = shared.HumanAgo(bi.LatestAt)
			sig["backupinfo"] = fmt.Sprintf("%d backups · %s total · latest %s",
				bi.Count, shared.HumanSize(bi.TotalSize), shared.HumanSize(bi.LatestSize))
		}
	}

	// 3. Engine telemetry driven by registered games
	for _, p := range h.registeredProfiles() {
		eng := h.gameEngine(p.ID)
		if eng == nil {
			continue
		}
		tele, err := eng.Telemetry(ctx)
		if err != nil {
			continue
		}

		if p.ID == domain.GameValheim {
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

		if tele.PlayersKnown {
			sig[p.Prefix+"_players"] = tele.Players
		}
		if tele.State != "" {
			sig[p.Prefix+"_state"] = tele.State
		}
		sig[p.Prefix+"_online"] = tele.Online
		if tele.Uptime != "" {
			sig[p.Prefix+"_uptime"] = tele.Uptime
		}
		if tele.CPU != "" {
			sig[p.Prefix+"_cpu"] = tele.CPU
		}
		if tele.Memory != "" {
			sig[p.Prefix+"_mem"] = tele.Memory
		}
		if tele.Loader != "" {
			sig[p.Prefix+"_loader"] = tele.Loader
		}
		if tele.PackName != "" {
			sig[p.Prefix+"_pack"] = tele.PackName
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
