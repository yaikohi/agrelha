package shared

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/gofiber/fiber/v2"
)

// SSEToast emits a Datastar SSE event patching the toast signal and any extra signals.
func SSEToast(c *fiber.Ctx, kind, msg string, extra map[string]any) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	sig := map[string]any{"toast": msg, "toastkind": kind, "showUpdates": false}
	maps.Copy(sig, extra)
	b, _ := json.Marshal(sig)
	return c.SendString(fmt.Sprintf("event: datastar-patch-signals\ndata: signals %s\n\n", string(b)))
}
