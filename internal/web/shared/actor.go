package shared

import (
	"agrelha/internal/domain"

	"github.com/gofiber/fiber/v2"
)

const PrincipalKey = "principal"

func Actor(c *fiber.Ctx) string {
	if a, ok := c.Locals("actor").(string); ok && a != "" {
		return a
	}
	return "-"
}

func PrincipalOf(c *fiber.Ctx) domain.Principal {
	if p, ok := c.Locals(PrincipalKey).(domain.Principal); ok {
		return p
	}
	return domain.Principal{}
}

func IsAdmin(c *fiber.Ctx) bool { return PrincipalOf(c).Admin }
