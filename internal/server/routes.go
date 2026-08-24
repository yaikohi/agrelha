package server

import (
	"context"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"

	efs "agrelha/cmd/web"
	"agrelha/cmd/web/pages"
)

func (s *FiberServer) RegisterFiberRoutes() {
	s.App.Get("/healthz", func(c *fiber.Ctx) error { return c.SendString("ok") })
	s.App.Use("/assets", filesystem.New(filesystem.Config{
		Root:       http.FS(efs.Files),
		PathPrefix: "assets",
	}))

	if s.auth != nil {
		s.App.Get("/login", func(c *fiber.Ctx) error { return render(c, pages.Login()) })
		s.App.Get("/auth/login", s.auth.Login)
		s.App.Get("/auth/callback", s.auth.Callback)
		s.App.Get("/auth/logout", s.auth.Logout)
	}

	app := s.App.Group("/")
	if s.auth != nil {
		app.Use(s.auth.Middleware())
	}

	app.Get("/", func(c *fiber.Ctx) error {
		return render(c, pages.Dashboard(s.cfg.GrafanaDashboardURL))
	})
	app.Get("/sse", s.sseDashboard)

	app.Post("/server/restart", s.guard("restart", func(ctx context.Context) error { return s.k8s.Restart(ctx) }))
	app.Post("/server/stop", s.guard("stop", func(ctx context.Context) error { return s.k8s.Scale(ctx, 0) }))
	app.Post("/server/start", s.guard("start", func(ctx context.Context) error { return s.k8s.Scale(ctx, 1) }))

	app.Get("/mods", s.modsPage)
	app.Post("/mods/install", s.modsInstall)
	app.Post("/mods/remove", s.modsRemove)

	app.Get("/admins", s.adminsPage)
	app.Post("/admins/grant", s.adminsGrant)
	app.Post("/admins/revoke", s.adminsRevoke)
}

func (s *FiberServer) actor(c *fiber.Ctx) string {
	if v, ok := c.Locals("actor").(string); ok && v != "" {
		return v
	}
	return "local"
}

// guard runs a k8s action with a timeout + audit, 503 if the client isn't wired.
func (s *FiberServer) guard(action string, fn func(context.Context) error) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if s.k8s == nil {
			return fiber.NewError(fiber.StatusServiceUnavailable, "k8s client not available")
		}
		ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		_ = s.store.RecordAudit(s.actor(c), action, "")
		_ = s.store.RecordEvent(action, s.actor(c))
		return c.SendStatus(fiber.StatusNoContent)
	}
}
