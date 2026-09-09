package dashboard

import (
	mcaccess "agrelha/internal/app/access"
	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"time"

	"agrelha/internal/infra/backups"
	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/store"
	"agrelha/internal/platform/config"
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

// Config defines dependencies for the dashboard page and main SSE loop.
type Config struct {
	Cfg           *config.Config
	Store         *store.Store
	K8s           *k8s.Client
	MCK8s         *k8s.Client
	MCInstances   *instances.InstanceManager
	MCAccess      *mcaccess.AccessManager
	ValheimGame   ports.Game
	MinecraftGame ports.Game
	Auth          ports.Auth
	Actor         func(*fiber.Ctx) string
	BackupInfo    func() (backups.Info, bool)
	ModUpdates    func(context.Context) []pages.ModUpdate
	PendingActive func(context.Context) bool
	InstanceStats func(context.Context, []domain.Instance) map[int]InstanceStat
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

	grafanaURL, valheimAddr, nodeName := "", "", ""
	if h.cfg.Cfg != nil {
		grafanaURL = h.cfg.Cfg.GrafanaDashboardURL
		valheimAddr = h.cfg.Cfg.ValheimAddress
		nodeName = h.cfg.Cfg.GameNodeName
	}

	return shared.Render(c, pages.Dashboard(grafanaURL, valheimAddr, nodeName, mcSummary, isAdmin, fk, fm))
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

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		lastListSig := "\x00"
		push := func() bool {
			var ups []pages.ModUpdate
			if h.cfg.ModUpdates != nil {
				ups = h.cfg.ModUpdates(ctx)
			}
			sig := h.TileSignals(ctx)
			sig["updates"] = len(ups)
			pending := false
			if h.cfg.PendingActive != nil {
				pending = h.cfg.PendingActive(ctx)
			}
			sig["updatePending"] = pending
			if err := sse.PatchSignals(w, sig); err != nil {
				reason = "client-gone"
				slog.Debug("sse write failed", "rid", id, "frame", "signals", "err", err)
				return false
			}
			tiles++
			metrics.SSEFrames.WithLabelValues("signals").Inc()

			if listSig := updatesSignature(ups); listSig != lastListSig {
				var buf bytes.Buffer
				if err := pages.UpdateList(ups).Render(ctx, &buf); err == nil {
					if err := sse.InnerElement(w, "#update-list", buf.String()); err != nil {
						reason = "client-gone"
						slog.Debug("sse write failed", "rid", id, "frame", "update-list", "err", err)
						return false
					}
					metrics.SSEFrames.WithLabelValues("update-list").Inc()
					lastListSig = listSig
				}
			}
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

func updatesSignature(ups []pages.ModUpdate) string {
	var b bytes.Buffer
	for _, u := range ups {
		b.WriteString(u.Key)
		b.WriteByte('@')
		b.WriteString(u.Latest)
		b.WriteByte(';')
	}
	return b.String()
}

// TileSignals computes the metric signals for live dashboard tiles.
func (h *Handler) TileSignals(ctx context.Context) map[string]any {
	sig := map[string]any{
		"players": "—", "cpu": "—", "mem": "—", "uptime": "—", "state": "unknown", "online": false, "backup": "—", "backupinfo": "",
		"mc_players": "—", "mc_cpu": "—", "mc_mem": "—", "mc_uptime": "—", "mc_state": "unknown", "mc_loader": "NeoForge",
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
	} else {
		if h.cfg.Store != nil {
			if n, err := h.cfg.Store.CountOnline(); err == nil {
				sig["players"] = n
			}
		}
		if h.cfg.K8s != nil {
			if ps, err := h.cfg.K8s.PodStatus(ctx); err == nil {
				if ps.Ready {
					sig["state"] = "Up"
				} else {
					sig["state"] = ps.Phase
				}
				sig["online"] = ps.Ready
				if !ps.StartedAt.IsZero() {
					sig["uptime"] = shared.HumanDuration(time.Since(ps.StartedAt))
				}
			}
			if cpu, mem, err := h.cfg.K8s.PodMetrics(ctx); err == nil {
				sig["cpu"] = fmt.Sprintf("%dm", cpu)
				sig["mem"] = fmt.Sprintf("%d Mi", mem)
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
	} else {
		if h.cfg.MCK8s != nil {
			if h.cfg.MCInstances != nil {
				if insts, err := h.cfg.MCInstances.ListInstances(ctx); err == nil && len(insts) > 0 {
					inst := insts[0]
					if inst.Loader == domain.LoaderFabric {
						sig["mc_loader"] = "Fabric"
					}
					if inst.PackDefined() && inst.Pack.Name != "" {
						sig["mc_pack"] = inst.Pack.Name
					}
				}
			}
		}
		if h.cfg.MCAccess != nil {
			if pl, err := h.cfg.MCAccess.OnlinePlayers(); err == nil {
				sig["mc_players"] = len(pl)
			}
		}
		if h.cfg.MCK8s != nil {
			if ps, err := h.cfg.MCK8s.PodStatus(ctx); err == nil {
				if ps.Ready {
					sig["mc_state"] = "Up"
				} else {
					sig["mc_state"] = ps.Phase
				}
				if !ps.StartedAt.IsZero() {
					sig["mc_uptime"] = shared.HumanDuration(time.Since(ps.StartedAt))
				}
			}
			if cpu, mem, err := h.cfg.MCK8s.PodMetrics(ctx); err == nil {
				sig["mc_cpu"] = fmt.Sprintf("%dm", cpu)
				sig["mc_mem"] = fmt.Sprintf("%d Mi", mem)
			}
		}
	}

	return sig
}
