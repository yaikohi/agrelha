package shared

import (
	"github.com/a-h/templ"
	"github.com/gofiber/fiber/v2"
)

// Render writes a templ component as the Fiber response body.
func Render(c *fiber.Ctx, component templ.Component) error {
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	c.Set(fiber.HeaderCacheControl, "no-store, must-revalidate")
	c.Set("Pragma", "no-cache")
	return component.Render(c.UserContext(), c.Response().BodyWriter())
}
