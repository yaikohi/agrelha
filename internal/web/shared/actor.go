package shared

import (
	"agrelha/internal/ports"

	"github.com/gofiber/fiber/v2"
)

// Actor returns the authenticated user/email stored in Locals, or "-" if unset.
func Actor(c *fiber.Ctx) string {
	if a, ok := c.Locals("actor").(string); ok && a != "" {
		return a
	}
	return "-"
}

// IsAdmin checks whether the current request is from an authenticated admin.
func IsAdmin(auth ports.Auth, c *fiber.Ctx) bool {
	if auth == nil {
		return true
	}
	return auth.IsAuthenticated(c)
}
