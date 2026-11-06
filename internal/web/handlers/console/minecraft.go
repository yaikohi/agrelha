package console

import (
	"bufio"
	"bytes"
	"fmt"
	"html"
	"strconv"
	"strings"

	"agrelha/internal/web/shared"
	"agrelha/internal/web/sse"

	"github.com/gofiber/fiber/v2"
)

// MCRconCommand executes a Minecraft console command via RCON and returns the output fragment via SSE.
func (h *Handler) MCRconCommand(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return shared.SSEToast(c, "err", "Instance manager unconfigured.", nil)
	}

	instanceNumber, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	var body struct {
		Cmd string `json:"cmd" form:"cmd"`
	}
	_ = c.BodyParser(&body)
	cmd := strings.TrimSpace(body.Cmd)
	if cmd == "" {
		cmd = strings.TrimSpace(c.FormValue("cmd"))
	}
	if cmd == "" {
		return shared.SSEToast(c, "err", "Command cannot be empty.", nil)
	}

	resp, err := h.cfg.MCInstances.ExecuteCommand(c.UserContext(), instanceNumber, cmd)
	if err != nil {
		resp = fmt.Sprintf("Error: %s", err.Error())
	}

	escapedCmd := html.EscapeString(cmd)
	escapedResp := html.EscapeString(resp)
	fragment := fmt.Sprintf(`
		<div class="border-t border-edge/40 pt-1 mt-1">
			<span class="font-pixel font-semibold text-accent">&gt; %s</span>
			<pre class="font-pixel whitespace-pre-wrap mt-0.5 text-ink-muted">%s</pre>
		</div>
	`, escapedCmd, escapedResp)

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	_ = sse.AppendElement(w, "#console-logs", fragment)
	return c.Send(buf.Bytes())
}

// MCLogsStream streams real-time deployment logs for a Minecraft instance over SSE.
func (h *Handler) MCLogsStream(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
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

	stream, err := h.cfg.MCInstances.InstanceLogs(c.UserContext(), instanceNumber, 100)
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
