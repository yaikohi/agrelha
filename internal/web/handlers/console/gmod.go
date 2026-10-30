package console

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"strconv"

	"agrelha/internal/web/sse"

	"github.com/gofiber/fiber/v2"
)

const gmodLogTail = 1000

func (h *Handler) GModLogsStream(c *fiber.Ctx) error {
	if h.cfg.GModInstances == nil {
		return sseMessage(c, "<p class=\"text-zinc-500\">Logs unavailable</p>")
	}

	instanceNumber, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid number")
	}

	stream, err := h.gmodStream(instanceNumber)
	if err != nil {
		return sseMessage(c, fmt.Sprintf("<p class=\"text-red-400\">Log stream error: %s</p>", html.EscapeString(err.Error())))
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer stream.Close()

		scanner := bufio.NewScanner(stream)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := html.EscapeString(scanner.Text())
			_ = sse.AppendElement(w, "#console-logs", fmt.Sprintf("<div class=\"text-zinc-300\">%s</div>", line))
		}
	})

	return nil
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
func (h *Handler) gmodStream(instanceNumber int) (io.ReadCloser, error) {
	if h.cfg.GModConsoleLog != nil {
		if rc, err := h.cfg.GModConsoleLog(context.Background(), instanceNumber, gmodLogTail); err == nil {
			return rc, nil
		}
	}
	return h.cfg.GModInstances.InstanceLogs(context.Background(), instanceNumber, gmodLogTail)
}
