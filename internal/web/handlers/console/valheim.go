package console

import (
	"bufio"
	"context"
	"fmt"
	"html"
	"log/slog"
	"time"

	"agrelha/internal/web/metrics"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
	"agrelha/internal/web/sse"

	"github.com/gofiber/fiber/v2"
)

// ValheimConsole renders the Valheim console/logs page.
func (h *Handler) ValheimConsole(c *fiber.Ctx) error {
	fk, fm := shared.TakeFlash(c)
	return shared.Render(c, pages.ValheimConsole(shared.IsAdmin(h.cfg.Auth, c), fk, fm))
}

// SSELogs streams live log tails into #logs. Supports ?server=valheim (default) or ?server=minecraft.
func (h *Handler) SSELogs(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	server := c.Query("server", "valheim")
	id := shared.RequestID(c)
	actor := h.cfg.Actor(c)
	slog.Info("sse open", "rid", id, "actor", actor, "stream", "logs", "server", server)

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
			slog.Info("sse close", "rid", id, "actor", actor, "stream", "logs", "server", server,
				"reason", reason, "log_lines", logLines, "dur_ms", time.Since(start).Milliseconds())
		}()

		client := h.cfg.K8s
		if server == "minecraft" {
			client = h.cfg.MCK8s
		}
		if client == nil {
			return
		}
		rc, err := client.StreamLogs(ctx, 50)
		if err != nil {
			slog.Warn("sse log stream unavailable", "rid", id, "server", server, "err", err)
			return
		}
		defer rc.Close()
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			el := fmt.Sprintf(`<div class="whitespace-pre-wrap">%s</div>`, html.EscapeString(sc.Text()))
			if err := sse.AppendElement(w, "#logs", el); err != nil {
				reason = "client-gone"
				slog.Debug("sse write failed", "rid", id, "frame", "log", "server", server, "err", err)
				return
			}
			logLines++
			metrics.SSEFrames.WithLabelValues("log").Inc()
		}
	})
	return nil
}

func (h *Handler) ServerRestart(c *fiber.Ctx) error {
	return h.guard("restart", "Restart triggered — the server is rolling.", func(ctx context.Context) error {
		return h.cfg.K8s.Restart(ctx)
	})(c)
}

func (h *Handler) ServerUpdate(c *fiber.Ctx) error {
	return h.guard("update", "Update triggered — restarting; the image installs any Valheim update on boot.", func(ctx context.Context) error {
		return h.cfg.K8s.Restart(ctx)
	})(c)
}

func (h *Handler) ServerStop(c *fiber.Ctx) error {
	return h.guard("stop", "Stopping the server — scaling to 0.", func(ctx context.Context) error {
		return h.cfg.K8s.Scale(ctx, 0)
	})(c)
}

func (h *Handler) ServerStart(c *fiber.Ctx) error {
	return h.guard("start", "Starting the server — scaling to 1.", func(ctx context.Context) error {
		return h.cfg.K8s.Scale(ctx, 1)
	})(c)
}
