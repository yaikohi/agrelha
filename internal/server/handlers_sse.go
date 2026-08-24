package server

import (
	"bufio"
	"context"
	"fmt"
	"html"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/sse"
	"agrelha/internal/valheim"
)

// sseDashboard streams tile signals (every 5s) + a live log tail over one SSE
// connection, using Fiber's native stream writer (fasthttp) — see internal/sse.
func (s *FiberServer) sseDashboard(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		lines := make(chan string, 128)
		if s.k8s != nil {
			go func() {
				rc, err := s.k8s.StreamLogs(ctx, 50)
				if err != nil {
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
			return // client gone
		}
		for {
			select {
			case <-ticker.C:
				if err := sse.PatchSignals(w, s.tileSignals(ctx)); err != nil {
					return
				}
			case ln := <-lines:
				el := fmt.Sprintf(`<div class="whitespace-pre-wrap">%s</div>`, html.EscapeString(ln))
				if err := sse.AppendElement(w, "#logs", el); err != nil {
					return
				}
			}
		}
	})
	return nil
}

func (s *FiberServer) tileSignals(ctx context.Context) map[string]any {
	sig := map[string]any{"players": "—", "cpu": "—", "mem": "—", "uptime": "—", "state": "unknown"}

	if s.cfg.ValheimStatusURL != "" {
		if st, err := valheim.FetchStatus(ctx, s.cfg.ValheimStatusURL); err == nil && st.Err == "" {
			sig["players"] = st.PlayerCount
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
