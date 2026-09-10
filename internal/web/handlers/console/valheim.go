package console

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"html"
	"log/slog"
	"strconv"
	"time"

	"agrelha/internal/ports"
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

		rt := h.cfg.ValheimRuntime
		ref := h.cfg.ValheimRef
		if server == "minecraft" {
			rt = h.cfg.MCRuntime
			ref = h.cfg.MCRef
		}
		if rt == nil {
			return
		}
		rc, err := rt.Logs(ctx, ref, ports.LogOptions{Tail: 50, Follow: true})
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
		return h.cfg.ValheimRuntime.Restart(ctx, h.cfg.ValheimRef)
	})(c)
}

func (h *Handler) ServerUpdate(c *fiber.Ctx) error {
	return h.guard("update", "Update triggered — restarting; the image installs any Valheim update on boot.", func(ctx context.Context) error {
		return h.cfg.ValheimRuntime.Restart(ctx, h.cfg.ValheimRef)
	})(c)
}

func (h *Handler) ServerStop(c *fiber.Ctx) error {
	return h.guard("stop", "Stopping the server — scaling to 0.", func(ctx context.Context) error {
		return h.cfg.ValheimRuntime.Stop(ctx, h.cfg.ValheimRef)
	})(c)
}

func (h *Handler) ServerStart(c *fiber.Ctx) error {
	return h.guard("start", "Starting the server — scaling to 1.", func(ctx context.Context) error {
		return h.cfg.ValheimRuntime.Start(ctx, h.cfg.ValheimRef)
	})(c)
}

// ValheimLogsStream streams real-time deployment logs for a Valheim instance over SSE.
func (h *Handler) ValheimLogsStream(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		var buf bytes.Buffer
		w := bufio.NewWriter(&buf)
		_ = sse.AppendElement(w, "#console-logs", "<p class=\"text-zinc-500\">Logs unavailable</p>")
		return c.Send(buf.Bytes())
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid number")
	}

	stream, err := h.cfg.ValheimInstances.InstanceLogs(context.Background(), num, 100)
	if err != nil {
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		var buf bytes.Buffer
		w := bufio.NewWriter(&buf)
		_ = sse.AppendElement(w, "#console-logs", fmt.Sprintf("<p class=\"text-red-400\">Log stream error: %s</p>", html.EscapeString(err.Error())))
		return c.Send(buf.Bytes())
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer stream.Close()

		scanner := bufio.NewScanner(stream)
		for scanner.Scan() {
			line := html.EscapeString(scanner.Text())
			frag := fmt.Sprintf("<div class=\"text-zinc-300\">%s</div>", line)
			_ = sse.AppendElement(w, "#console-logs", frag)
		}
	})

	return nil
}

