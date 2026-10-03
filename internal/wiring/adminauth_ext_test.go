package wiring_test

import (
	"agrelha/internal/domain"

	"github.com/gofiber/fiber/v2"
)

type testAdminAuth struct{}

func (testAdminAuth) Identify(c *fiber.Ctx) (domain.Identity, bool) {
	return domain.Identity{
		Subject: "test-admin",
		Email:   "admin@example.com",
		Roles:   []domain.Role{domain.RoleAdmin, domain.RoleUser},
	}, true
}
func (testAdminAuth) Login(c *fiber.Ctx) error    { return nil }
func (testAdminAuth) Callback(c *fiber.Ctx) error { return nil }
func (testAdminAuth) Logout(c *fiber.Ctx) error   { return nil }
