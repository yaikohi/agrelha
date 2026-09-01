package server

import (
	"bufio"
	"context"
	"fmt"
	"html"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/backups"
	"agrelha/internal/metrics"
	"agrelha/internal/sse"
)

// sseDashboard streams tile signals (every 5s) + a live log tail over one SSE
// connection, using Fiber's native stream writer (fasthttp) — see internal/sse.
func (s *FiberServer) sseDashboard(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	id := rid(c)
	actor := s.actor(c)
	slog.Info("sse open", "rid", id, "actor", actor)

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		metrics.SSEActive.Inc()
		metrics.SSEOpened.Inc()
		start := time.Now()
		var tiles, logLines int
		reason := "loop-exit"
		defer func() {
			metrics.SSEActive.Dec()
			metrics.SSEClosed.WithLabelValues(reason).Inc()
			slog.Info("sse close", "rid", id, "actor", actor,
				"reason", reason, "tiles", tiles, "log_lines", logLines,
				"dur_ms", time.Since(start).Milliseconds())
		}()

		lines := make(chan string, 128)
		if s.k8s != nil {
			go func() {
				rc, err := s.k8s.StreamLogs(ctx, 50)
				if err != nil {
					slog.Warn("sse log stream unavailable", "rid", id, "err", err)
					return
				}
				defer rc.Close()
				sc := bufio.NewScanner(rc)
				sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
				for sc.Scan() {
					select {
					case lines <- sc.Text():
					case <-ctx.Done():
						return
					}
				}
			}()
		}

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		if err := sse.PatchSignals(w, s.tileSignals(ctx)); err != nil {
			reason = "client-gone"
			slog.Debug("sse write failed", "rid", id, "frame", "signals", "err", err)
			return
		}
		tiles++
		metrics.SSEFrames.WithLabelValues("signals").Inc()
		for {
			select {
			case <-ticker.C:
				if err := sse.PatchSignals(w, s.tileSignals(ctx)); err != nil {
					reason = "client-gone"
					slog.Debug("sse write failed", "rid", id, "frame", "signals", "err", err)
					return
				}
				tiles++
				metrics.SSEFrames.WithLabelValues("signals").Inc()
			case ln := <-lines:
				el := fmt.Sprintf(`<div class="whitespace-pre-wrap">%s</div>`, html.EscapeString(ln))
				if err := sse.AppendElement(w, "#logs", el); err != nil {
					reason = "client-gone"
					slog.Debug("sse write failed", "rid", id, "frame", "log", "err", err)
					return
				}
				logLines++
				metrics.SSEFrames.WithLabelValues("log").Inc()
			}
		}
	})
	return nil
}

func (s *FiberServer) tileSignals(ctx context.Context) map[string]any {
	sig := map[string]any{"players": "—", "cpu": "—", "mem": "—", "uptime": "—", "state": "unknown", "backup": "—", "backupinfo": ""}

	if s.store != nil {
		if n, err := s.store.CountOnline(); err == nil {
			sig["players"] = n
		}
	}
	if s.cfg.BackupsDir != "" {
		if bi, ok := s.backupInfo(); ok && bi.Count > 0 {
			sig["backup"] = humanAgo(bi.LatestAt)
			sig["backupinfo"] = fmt.Sprintf("%d backups · %s total · latest %s",
				bi.Count, humanSize(bi.TotalSize), humanSize(bi.LatestSize))
		}
	}
	if s.k8s != nil {
		if ps, err := s.k8s.PodStatus(ctx); err == nil {
			if ps.Ready {
				sig["state"] = "Up"
			} else {
				sig["state"] = ps.Phase
			}
			if !ps.StartedAt.IsZero() {
				sig["uptime"] = humanDuration(time.Since(ps.StartedAt))
			}
		}
		if cpu, mem, err := s.k8s.PodMetrics(ctx); err == nil {
			sig["cpu"] = fmt.Sprintf("%dm", cpu)
			sig["mem"] = fmt.Sprintf("%d Mi", mem)
		}
	}
	return sig
}

func (s *FiberServer) backupInfo() (backups.Info, bool) {
	s.bkMu.Lock()
	if !s.bkAt.IsZero() && time.Since(s.bkAt) < time.Minute {
		i, ok := s.bkInfo, s.bkOK
		s.bkMu.Unlock()
		return i, ok
	}
	s.bkAt = time.Now()
	s.bkMu.Unlock()

	type res struct {
		i  backups.Info
		ok bool
	}
	ch := make(chan res, 1)
	go func() {
		i, err := backups.Stat(s.cfg.BackupsDir)
		ch <- res{i, err == nil}
	}()
	var out res
	select {
	case out = <-ch:
	case <-time.After(3 * time.Second):
	}

	s.bkMu.Lock()
	s.bkInfo, s.bkOK = out.i, out.ok
	s.bkMu.Unlock()
	return out.i, out.ok
}

func humanAgo(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	return humanDuration(d) + " ago"
}

func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func humanDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	m := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, h)
	}
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
