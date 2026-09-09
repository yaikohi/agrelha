package web

import (
	"net/http"

	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"

	"agrelha/internal/ports"
	"agrelha/internal/web/handlers/access"
	backupshttp "agrelha/internal/web/handlers/backups"
	consolehttp "agrelha/internal/web/handlers/console"
	contenthttp "agrelha/internal/web/handlers/content"
	dashboardhttp "agrelha/internal/web/handlers/dashboard"
	instanceshttp "agrelha/internal/web/handlers/instances"
	wizardhttp "agrelha/internal/web/handlers/wizard"
	"agrelha/internal/web/metrics"
	"agrelha/internal/web/shared"
)

// ServerConfig defines the handlers and authentication provider used to mount web routes.
type ServerConfig struct {
	Auth      ports.Auth
	Access    *access.Handler
	Backups   *backupshttp.Handler
	Console   *consolehttp.Handler
	Content   *contenthttp.Handler
	Dashboard *dashboardhttp.Handler
	Instances *instanceshttp.Handler
	Wizard    *wizardhttp.Handler
}

// RegisterRoutes attaches middleware and all endpoints (public and protected) to the Fiber app.
func RegisterRoutes(app *fiber.App, cfg ServerConfig) {
	app.Use(shared.RequestLogger())
	app.Get("/healthz", func(c *fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/metrics", adaptor.HTTPHandler(metrics.Handler()))
	app.Use("/assets", filesystem.New(filesystem.Config{
		Root:       http.FS(Files),
		PathPrefix: "assets",
	}))

	if cfg.Auth != nil {
		app.Get("/login", func(c *fiber.Ctx) error {
			target := "/auth/login"
			if q := c.Request().URI().QueryString(); len(q) > 0 {
				target += "?" + string(q)
			}
			return c.Redirect(target, fiber.StatusFound)
		})
		app.Get("/auth/login", cfg.Auth.Login)
		app.Post("/auth/login", cfg.Auth.Login)
		app.Get("/auth/callback", cfg.Auth.Callback)
		app.Get("/auth/logout", cfg.Auth.Logout)
	}

	// Public routes (accessible by LAN / WireGuard players)
	if cfg.Dashboard != nil {
		app.Get("/", cfg.Dashboard.DashboardPage)
		app.Get("/sse", cfg.Dashboard.SSEMain)
	}
	if cfg.Content != nil {
		cfg.Content.RegisterPublic(app)
	}
	if cfg.Instances != nil {
		cfg.Instances.RegisterPublic(app)
	}

	// Protected routes (admin authentication required)
	protected := app.Group("/")
	if cfg.Auth != nil {
		protected.Use(cfg.Auth.Middleware())
	}

	if cfg.Access != nil {
		cfg.Access.Register(protected)
	}
	if cfg.Backups != nil {
		cfg.Backups.Register(protected)
	}
	if cfg.Console != nil {
		cfg.Console.Register(protected)
	}
	if cfg.Content != nil {
		cfg.Content.RegisterProtected(protected)
	}
	if cfg.Instances != nil {
		cfg.Instances.RegisterProtected(protected)
	}
	if cfg.Wizard != nil {
		cfg.Wizard.Register(protected)
	}
}

// New creates a new Fiber application with the specified routes mounted.
func New(cfg ServerConfig) *fiber.App {
	app := fiber.New(fiber.Config{
		ServerHeader: "agrelha",
		AppName:      "agrelha",
	})
	RegisterRoutes(app, cfg)
	return app
}
