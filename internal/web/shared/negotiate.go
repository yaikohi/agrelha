package shared

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

// WantsStream reports whether the caller is Datastar (or any SSE client) and so
// expects a text/event-stream response.
func WantsStream(c *fiber.Ctx) bool {
	return c.Get("Datastar-Request") == "true" || strings.Contains(c.Get("Accept"), "text/event-stream")
}

// WantsJSON reports whether the caller wants a JSON document. Datastar sends
// "Accept: text/event-stream, text/html, application/json", so a plain substring
// test on Accept matches it too; stream clients are excluded first.
func WantsJSON(c *fiber.Ctx) bool {
	if WantsStream(c) {
		return false
	}
	return strings.Contains(c.Get("Accept"), "application/json")
}
