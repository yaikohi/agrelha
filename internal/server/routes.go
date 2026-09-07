package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"

	efs "agrelha/cmd/web"
	"agrelha/cmd/web/pages"
	"agrelha/internal/metrics"
)

func (s *FiberServer) RegisterFiberRoutes() {
	s.App.Use(requestLogger())
	s.App.Get("/healthz", func(c *fiber.Ctx) error { return c.SendString("ok") })
	s.App.Get("/metrics", adaptor.HTTPHandler(metrics.Handler()))
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
		fk, fm := takeFlash(c)
		return render(c, pages.Dashboard(s.cfg.GrafanaDashboardURL, fk, fm))
	})
	app.Get("/sse", s.sseMain)
	app.Get("/sse/logs", s.sseLogs)
	app.Get("/img", s.imageProxy)

	app.Post("/server/restart", s.guard("restart", "Restart triggered — the server is rolling.", func(ctx context.Context) error { return s.k8s.Restart(ctx) }))
	app.Post("/server/update", s.guard("update", "Update triggered — restarting; the image installs any Valheim update on boot.", func(ctx context.Context) error { return s.k8s.Restart(ctx) }))
	app.Post("/server/stop", s.guard("stop", "Stopping the server — scaling to 0.", func(ctx context.Context) error { return s.k8s.Scale(ctx, 0) }))
	app.Post("/server/start", s.guard("start", "Starting the server — scaling to 1.", func(ctx context.Context) error { return s.k8s.Scale(ctx, 1) }))

	app.Get("/mods", s.modsPage)
	app.Get("/mods/export", s.modpackExport)
	app.Get("/mods/:namespace/:name", s.modDetail)
	app.Post("/mods/install", s.modsInstall)
	app.Post("/mods/remove", s.modsRemove)
	app.Post("/mods/update", s.modsUpdateSelected)
	app.Post("/mods/update-all", s.modsUpdateAll)

	app.Get("/configs", s.configsPage)
	app.Get("/configs/new", s.configNew)
	app.Get("/configs/edit", s.configEdit)
	app.Post("/configs/save", s.configSave)
	app.Post("/configs/delete", s.configDelete)

	app.Get("/history", s.historyPage)

	app.Get("/admins", s.adminsPage)
	app.Post("/admins/grant", s.adminsGrant)
	app.Post("/admins/revoke", s.adminsRevoke)

	// --- Minecraft NeoForge routes ---
	app.Get("/minecraft/mods", s.mcModsPage)
	app.Get("/minecraft/mods/export", s.mcModpackExport)
	app.Get("/minecraft/access", s.mcAccessPage)
	app.Get("/minecraft/configs", s.mcConfigsPage)
	app.Get("/minecraft/configs/new", s.mcConfigNew)
	app.Get("/minecraft/configs/edit", s.mcConfigEdit)
	app.Post("/minecraft/configs/save", s.mcConfigSave)
	app.Post("/minecraft/configs/delete", s.mcConfigDelete)

	app.Get("/api/minecraft/mods/search", s.mcModsSearch)
	app.Post("/api/minecraft/mods/install", s.mcModsInstall)
	app.Post("/api/minecraft/mods/remove", s.mcModsRemove)
	app.Post("/api/minecraft/version/set", s.mcVersionSet)
	app.Post("/api/minecraft/slot/switch", s.mcSlotSwitch)
	app.Post("/api/minecraft/access/op", s.mcAccessGrantOp)
	app.Post("/api/minecraft/access/deop", s.mcAccessRevokeOp)
	app.Post("/api/minecraft/access/whitelist/add", s.mcAccessAddWhitelist)
	app.Post("/api/minecraft/access/whitelist/remove", s.mcAccessRemoveWhitelist)
	app.Post("/api/minecraft/access/whitelist/toggle", s.mcAccessWhitelistToggle)
	app.Get("/api/minecraft/players", s.mcOnlinePlayers)
	app.Get("/api/minecraft/modpacks/search", s.mcModpacksSearch)
	app.Get("/api/minecraft/modpacks/:id", s.mcModpackGet)
	app.Post("/api/minecraft/modpacks/switch", s.mcModpackSwitch)
	app.Post("/api/minecraft/loader/switch", s.mcLoaderSwitch)

	app.Post("/minecraft/server/restart", s.guardMC("mc-restart", "Minecraft restart triggered — server is rolling.", func(ctx context.Context) error { return s.mck8s.Restart(ctx) }))
	app.Post("/minecraft/server/stop", s.guardMC("mc-stop", "Stopping Minecraft server — scaling to 0.", func(ctx context.Context) error { return s.mck8s.Scale(ctx, 0) }))
	app.Post("/minecraft/server/start", s.guardMC("mc-start", "Starting Minecraft server — scaling to 1.", func(ctx context.Context) error { return s.mck8s.Scale(ctx, 1) }))
}

func (s *FiberServer) actor(c *fiber.Ctx) string {
	if v, ok := c.Locals("actor").(string); ok && v != "" {
		return v
	}
	return "local"
}

func (s *FiberServer) guard(action, okMsg string, fn func(context.Context) error) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if s.k8s == nil {
			metrics.ControlActions.WithLabelValues(action, "disabled").Inc()
			return sseToast(c, "err", "Imperative plane disabled — no cluster access.", nil)
		}
		ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			metrics.ControlActions.WithLabelValues(action, "error").Inc()
			return sseToast(c, "err", action+" failed: "+err.Error(), nil)
		}
		metrics.ControlActions.WithLabelValues(action, "ok").Inc()
		_ = s.store.RecordAudit(s.actor(c), action, "")
		_ = s.store.RecordEvent(action, s.actor(c))
		return sseToast(c, "ok", okMsg, nil)
	}
}

func (s *FiberServer) guardMC(action, okMsg string, fn func(context.Context) error) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if s.mck8s == nil {
			metrics.ControlActions.WithLabelValues(action, "disabled").Inc()
			return sseToast(c, "err", "Minecraft imperative plane disabled — no cluster access.", nil)
		}
		ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			metrics.ControlActions.WithLabelValues(action, "error").Inc()
			slog.Error("minecraft control action failed", "action", action, "err", err)
			return sseToast(c, "err", action+" failed: "+err.Error(), nil)
		}
		metrics.ControlActions.WithLabelValues(action, "ok").Inc()
		_ = s.store.RecordAudit(s.actor(c), action, "")
		_ = s.store.RecordEvent(action, s.actor(c))
		return sseToast(c, "ok", okMsg, nil)
	}
}
