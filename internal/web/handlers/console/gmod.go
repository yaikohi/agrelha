package console

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strconv"

	"agrelha/internal/web/sse"

	"github.com/gofiber/fiber/v2"
)

const gmodLogTail = 1000

func (h *Handler) GModLogsStream(c *fiber.Ctx) error {
	if h.cfg.GModInstances == nil {
		return sseMessage(c, "<p class=\"text-ink-faint\">Logs unavailable</p>")
	}

	instanceNumber, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid number")
	}

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := h.gmodStream(ctx, instanceNumber)
	if err != nil {
		cancel()
		return sseMessage(c, logStreamError(err))
	}

	return h.streamLogs(c, stream, "#console-logs", cancel)
}

func sseMessage(c *fiber.Ctx, frag string) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	_ = sse.AppendElement(w, "#console-logs", frag)
	return c.Send(buf.Bytes())
}

// gmodStream prefers the server's own console log. The image sends srcds
// stdout to /dev/null, so the runtime stream carries only steamcmd output and
// never the game itself; the file is the only place the console exists. A
// world that has not written one yet falls back, so the tab is never empty.
func (h *Handler) gmodStream(ctx context.Context, instanceNumber int) (io.ReadCloser, error) {
	if h.cfg.GModConsoleLog != nil {
		if rc, err := h.cfg.GModConsoleLog(ctx, instanceNumber, gmodLogTail); err == nil {
			return rc, nil
		}
	}
	return h.cfg.GModInstances.InstanceLogs(ctx, instanceNumber, gmodLogTail)
}

