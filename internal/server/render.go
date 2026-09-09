package server

import (
	"agrelha/internal/web/shared"

	"github.com/a-h/templ"
	"github.com/gofiber/fiber/v2"
)

// render writes a templ component as the Fiber response body.
func render(c *fiber.Ctx, component templ.Component) error {
	return shared.Render(c, component)
}
