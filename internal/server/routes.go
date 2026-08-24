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
	// Unauthenticated: health + static assets + auth handshake.
	s.App.Get("/healthz", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})
	s.App.Use("/assets", filesystem.New(filesystem.Config{
		Root:       http.FS(efs.Files),
		PathPrefix: "assets",
	}))

	if s.auth != nil {
		s.App.Get("/auth/login", s.auth.Login)
		s.App.Get("/auth/callback", s.auth.Callback)
		s.App.Get("/auth/logout", s.auth.Logout)
	}

	// Everything below requires a session (when auth is configured).
	app := s.App.Group("/")
	if s.auth != nil {
		app.Use(s.auth.Middleware())
	}

	app.Get("/", func(c *fiber.Ctx) error {
		return render(c, pages.Dashboard(s.cfg.GrafanaDashboardURL))
	})

	// --- imperative plane (step③ fleshes out the UI + SSE) ---
	app.Post("/server/restart", s.guard(func(ctx context.Context) error { return s.k8s.Restart(ctx) }))
	app.Post("/server/stop", s.guard(func(ctx context.Context) error { return s.k8s.Scale(ctx, 0) }))
	app.Post("/server/start", s.guard(func(ctx context.Context) error { return s.k8s.Scale(ctx, 1) }))

	// TODO(step③):
	//   GET  /sse           -> Datastar SSE: metrics tiles + live log tail
	//   GET  /mods          -> Thunderstore search + installed list
	//   POST /mods/install  -> resolve deps -> commit valheim-mods.yaml -> push
	//   GET  /admins        -> roster picker
	//   POST /admins/grant  -> commit valheim-admins.yaml -> push -> restart
}

// guard runs a k8s action with a timeout, 503 if the client isn't wired.
func (s *FiberServer) guard(fn func(context.Context) error) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if s.k8s == nil {
			return fiber.NewError(fiber.StatusServiceUnavailable, "k8s client not available")
		}
		ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}
