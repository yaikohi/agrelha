package console

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"html"
	"strconv"

	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
	"agrelha/internal/web/sse"

	"github.com/gofiber/fiber/v2"
)

// ValheimConsole renders the Valheim console/logs page.
func (h *Handler) ValheimConsole(c *fiber.Ctx) error {
	fk, fm := shared.TakeFlash(c)
	return shared.Render(c, pages.ValheimConsole(shared.IsAdmin(c), fk, fm))
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
		_ = sse.AppendElement(w, "#console-logs", "<p class=\"text-ink-faint\">Logs unavailable</p>")
		return c.Send(buf.Bytes())
	}

	instanceNumber, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid number")
	}

	stream, err := h.cfg.ValheimInstances.InstanceLogs(c.UserContext(), instanceNumber, 100)
	if err != nil {
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		var buf bytes.Buffer
		w := bufio.NewWriter(&buf)
		_ = sse.AppendElement(w, "#console-logs", fmt.Sprintf("<p class=\"rounded-control bg-surface px-2 py-1 text-danger\">Log stream error: %s</p>", html.EscapeString(err.Error())))
		return c.Send(buf.Bytes())
	}

	return h.streamLogs(c, stream, "#console-logs")
}
