package console

import (
	"bufio"
	"bytes"
	"context"
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
	if h.cfg.MCInstances == nil || h.cfg.MCRconPool == nil {
		return shared.SSEToast(c, "err", "Instance manager or RCON pool unconfigured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	inst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return shared.SSEToast(c, "err", "Instance not found.", nil)
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

	addr := fmt.Sprintf("%s.minecraft-modded.svc.cluster.local:25575", inst.ServiceName())
	client := h.cfg.MCRconPool.ClientFor(addr)

	resp, err := client.Execute(cmd)
	if err != nil {
		resp = fmt.Sprintf("Error: %s", err.Error())
	}

	escapedCmd := html.EscapeString(cmd)
	escapedResp := html.EscapeString(resp)
	fragment := fmt.Sprintf(`
		<div class="border-t border-zinc-800/40 pt-1 mt-1">
			<span class="text-emerald-400 font-semibold">&gt; %s</span>
			<pre class="text-zinc-300 font-mono whitespace-pre-wrap mt-0.5">%s</pre>
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
	if h.cfg.MCInstances == nil || h.cfg.MCK8s == nil {
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

	inst, err := h.cfg.MCInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Not found")
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		stream, err := h.cfg.MCK8s.StreamDeploymentLogs(context.Background(), inst.DeploymentName(), 100)
		if err != nil {
			_ = sse.AppendElement(w, "#console-logs", fmt.Sprintf("<p class=\"text-red-400\">Log stream error: %s</p>", html.EscapeString(err.Error())))
			return
		}
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
