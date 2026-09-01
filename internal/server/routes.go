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
	app.Get("/img", s.imageProxy)

	app.Post("/server/restart", s.guard("restart", "Restart triggered — the server is rolling.", func(ctx context.Context) error { return s.k8s.Restart(ctx) }))
	app.Post("/server/update", s.guard("update", "Update triggered — restarting; the image installs any Valheim update on boot.", func(ctx context.Context) error { return s.k8s.Restart(ctx) }))
	app.Post("/server/stop", s.guard("stop", "Stopping the server…", func(ctx context.Context) error { return s.k8s.Scale(ctx, 0) }))
	app.Post("/server/start", s.guard("start", "Starting the server…", func(ctx context.Context) error { return s.k8s.Scale(ctx, 1) }))

	app.Get("/mods", s.modsPage)
	app.Get("/mods/export", s.modpackExport)
	app.Get("/mods/:namespace/:name", s.modDetail)
	app.Post("/mods/install", s.modsInstall)
	app.Post("/mods/remove", s.modsRemove)

	app.Get("/configs", s.configsPage)
	app.Get("/configs/new", s.configNew)
	app.Get("/configs/edit", s.configEdit)
	app.Post("/configs/save", s.configSave)
	app.Post("/configs/delete", s.configDelete)

	app.Get("/history", s.historyPage)

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

func (s *FiberServer) guard(action, toast string, fn func(context.Context) error) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if s.k8s == nil {
			return c.JSON(fiber.Map{"toast": "Imperative plane disabled — no cluster access."})
		}
		ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			return c.JSON(fiber.Map{"toast": "Failed: " + err.Error()})
		}
		_ = s.store.RecordAudit(s.actor(c), action, "")
		_ = s.store.RecordEvent(action, s.actor(c))
		return c.JSON(fiber.Map{"toast": toast})
	}
}
