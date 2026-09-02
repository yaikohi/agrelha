package server

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"html"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/cmd/web/pages"
	"agrelha/internal/backups"
	"agrelha/internal/metrics"
	"agrelha/internal/sse"
)

// sseMain streams tile signals + the available-updates badge/list (every 5s)
// over one SSE connection. Mounted on the shared layout, so every page gets the
// live tiles + update indicator. The log tail is a separate stream (sseLogs)
// opened only by the dashboard. Uses Fiber's native stream writer (fasthttp).
func (s *FiberServer) sseMain(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	id := rid(c)
	actor := s.actor(c)
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
			ups := s.modUpdates(ctx)
			sig := s.tileSignals(ctx)
			sig["updates"] = len(ups)
			sig["updatePending"] = s.pendingActive(ctx)
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

// sseLogs streams only the live log tail into #logs. Dashboard-only, so other
// pages don't pay for a log stream they don't render.
func (s *FiberServer) sseLogs(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	id := rid(c)
	actor := s.actor(c)
	slog.Info("sse open", "rid", id, "actor", actor, "stream", "logs")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		metrics.SSEActive.Inc()
		metrics.SSEOpened.Inc()
		start := time.Now()
		var logLines int
		reason := "loop-exit"
		defer func() {
			metrics.SSEActive.Dec()
			metrics.SSEClosed.WithLabelValues(reason).Inc()
			slog.Info("sse close", "rid", id, "actor", actor, "stream", "logs",
				"reason", reason, "log_lines", logLines, "dur_ms", time.Since(start).Milliseconds())
		}()

		if s.k8s == nil {
			return
		}
		rc, err := s.k8s.StreamLogs(ctx, 50)
		if err != nil {
			slog.Warn("sse log stream unavailable", "rid", id, "err", err)
			return
		}
		defer rc.Close()
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			el := fmt.Sprintf(`<div class="whitespace-pre-wrap">%s</div>`, html.EscapeString(sc.Text()))
			if err := sse.AppendElement(w, "#logs", el); err != nil {
				reason = "client-gone"
				slog.Debug("sse write failed", "rid", id, "frame", "log", "err", err)
				return
			}
			logLines++
			metrics.SSEFrames.WithLabelValues("log").Inc()
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
