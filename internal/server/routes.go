package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"

	"agrelha/internal/web"
	"agrelha/internal/web/metrics"
)

func (s *FiberServer) RegisterFiberRoutes() {
	s.App.Use(requestLogger())
	s.App.Get("/healthz", func(c *fiber.Ctx) error { return c.SendString("ok") })
	s.App.Get("/metrics", adaptor.HTTPHandler(metrics.Handler()))
	s.App.Use("/assets", filesystem.New(filesystem.Config{
		Root:       http.FS(web.Files),
		PathPrefix: "assets",
	}))

	if s.auth != nil {
		s.App.Get("/login", func(c *fiber.Ctx) error {
			target := "/auth/login"
			if q := c.Request().URI().QueryString(); len(q) > 0 {
				target += "?" + string(q)
			}
			return c.Redirect(target, fiber.StatusFound)
		})
		s.App.Get("/auth/login", s.auth.Login)
		s.App.Post("/auth/login", s.auth.Login)
		s.App.Get("/auth/callback", s.auth.Callback)
		s.App.Get("/auth/logout", s.auth.Logout)
	}

	// Public routes (LAN / WireGuard players)
	s.App.Get("/", func(c *fiber.Ctx) error {
		return s.ensureDashboardHandler().DashboardPage(c)
	})
	s.App.Get("/sse", func(c *fiber.Ctx) error {
		return s.ensureDashboardHandler().SSEMain(c)
	})
	s.App.Get("/img", s.imageProxy)
	s.App.Get("/mods/export", s.modpackExport)
	s.App.Get("/api/minecraft/:num<int>/mods/export", s.mcInstanceExport)

	// Protected routes (admin authentication required)
	app := s.App.Group("/")
	if s.auth != nil {
		app.Use(s.auth.Middleware())
	}
	app.Get("/sse/logs", s.sseLogs)
	app.Get("/valheim", s.valheimConsole)

	app.Post("/server/restart", s.guard("restart", "Restart triggered — the server is rolling.", func(ctx context.Context) error {
		if s.valheimRuntime != nil {
			return s.valheimRuntime.Restart(ctx, s.valheimRef)
		}
		return s.k8s.Restart(ctx)
	}))
	app.Post("/server/update", s.guard("update", "Update triggered — restarting; the image installs any Valheim update on boot.", func(ctx context.Context) error {
		if s.valheimRuntime != nil {
			return s.valheimRuntime.Restart(ctx, s.valheimRef)
		}
		return s.k8s.Restart(ctx)
	}))
	app.Post("/server/stop", s.guard("stop", "Stopping the server — scaling to 0.", func(ctx context.Context) error {
		if s.valheimRuntime != nil {
			return s.valheimRuntime.Stop(ctx, s.valheimRef)
		}
		return s.k8s.Scale(ctx, 0)
	}))
	app.Post("/server/start", s.guard("start", "Starting the server — scaling to 1.", func(ctx context.Context) error {
		if s.valheimRuntime != nil {
			return s.valheimRuntime.Start(ctx, s.valheimRef)
		}
		return s.k8s.Scale(ctx, 1)
	}))

	app.Get("/mods", s.modsPage)
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

	// --- Minecraft Multi-Instance routes ---
	app.Get("/minecraft", s.mcDashboard)
	app.Get("/minecraft/create", s.mcWizardPage)
	app.Get("/minecraft/provisioning/:num", s.mcProvisioningPage)
	app.Get("/api/minecraft/provisioning/:num/stream", s.mcProvisioningStream)
	app.Get("/api/minecraft/wizard/modpacks/search", s.mcWizardModpacksSearch)
	app.Post("/api/minecraft/wizard/modpacks/search", s.mcWizardModpacksSearch)
	app.Get("/api/minecraft/wizard/mods/search", s.mcWizardModsSearch)
	app.Post("/api/minecraft/wizard/mods/search", s.mcWizardModsSearch)
	app.Post("/api/minecraft/wizard/cart/check", s.mcWizardCartCheck)
	app.Post("/api/minecraft/wizard/create", s.mcWizardCreate)
	app.Post("/api/minecraft/wizard/import", s.mcWizardImport)
	app.Post("/api/minecraft/instances", s.mcInstanceCreate)
	app.Post("/api/minecraft/instances/:num/start", s.mcInstanceStart)
	app.Post("/api/minecraft/instances/:num/stop", s.mcInstanceStop)
	app.Post("/api/minecraft/instances/:num/restart", s.mcInstanceRestart)
	app.Delete("/api/minecraft/instances/:num", s.mcInstanceDelete)

	// --- Legacy/Per-instance routes ---
	app.Get("/minecraft/mods", func(c *fiber.Ctx) error {
		if s.mcInstances != nil {
			if insts, err := s.mcInstances.ListInstances(c.UserContext()); err == nil && len(insts) > 0 {
				return c.Redirect(fmt.Sprintf("/minecraft/%d/mods", insts[0].Number), fiber.StatusTemporaryRedirect)
			}
		}
		return c.Redirect("/minecraft", fiber.StatusTemporaryRedirect)
	})
	app.Get("/minecraft/access", s.mcAccessPage)
	app.Get("/minecraft/configs", func(c *fiber.Ctx) error {
		if s.mcInstances != nil {
			if insts, err := s.mcInstances.ListInstances(c.UserContext()); err == nil && len(insts) > 0 {
				return c.Redirect(fmt.Sprintf("/minecraft/%d/configs", insts[0].Number), fiber.StatusTemporaryRedirect)
			}
		}
		return c.Redirect("/minecraft", fiber.StatusTemporaryRedirect)
	})
	app.Get("/minecraft/configs/new", s.mcConfigNew)
	app.Get("/minecraft/configs/edit", s.mcConfigEdit)
	app.Post("/minecraft/configs/save", s.mcConfigSave)
	app.Post("/minecraft/configs/delete", s.mcConfigDelete)

	app.Post("/api/minecraft/access/op", s.mcAccessGrantOp)
	app.Post("/api/minecraft/access/deop", s.mcAccessRevokeOp)
	app.Post("/api/minecraft/access/whitelist/add", s.mcAccessAddWhitelist)
	app.Post("/api/minecraft/access/whitelist/remove", s.mcAccessRemoveWhitelist)
	app.Post("/api/minecraft/access/whitelist/toggle", s.mcAccessWhitelistToggle)

	app.Post("/minecraft/server/restart", s.guardMC("mc-restart", "Minecraft restart triggered — server is rolling.", func(ctx context.Context) error {
		if s.mcRuntime != nil {
			return s.mcRuntime.Restart(ctx, s.mcRef)
		}
		return s.mck8s.Restart(ctx)
	}))
	app.Post("/minecraft/server/stop", s.guardMC("mc-stop", "Stopping Minecraft server — scaling to 0.", func(ctx context.Context) error {
		if s.mcRuntime != nil {
			return s.mcRuntime.Stop(ctx, s.mcRef)
		}
		return s.mck8s.Scale(ctx, 0)
	}))
	app.Post("/minecraft/server/start", s.guardMC("mc-start", "Starting Minecraft server — scaling to 1.", func(ctx context.Context) error {
		if s.mcRuntime != nil {
			return s.mcRuntime.Start(ctx, s.mcRef)
		}
		return s.mck8s.Scale(ctx, 1)
	}))

	// --- Per-instance detail & controls ---
	app.Get("/minecraft/:num<int>", func(c *fiber.Ctx) error {
		return c.Redirect(fmt.Sprintf("/minecraft/%s/overview", c.Params("num")))
	})
	app.Get("/minecraft/:num<int>/:tab", s.mcInstancePage)
	app.Post("/api/minecraft/:num<int>/rcon", s.mcInstanceRcon)
	app.Get("/api/minecraft/:num<int>/logs/stream", s.mcInstanceLogsStream)
	app.Post("/api/minecraft/:num<int>/backups/create", s.mcInstanceBackupCreate)
	app.Post("/api/minecraft/:num<int>/backups/restore-inplace", s.mcInstanceBackupRestoreInPlace)
	app.Post("/api/minecraft/:num<int>/backups/restore-new", s.mcInstanceBackupRestoreNew)
	app.Get("/api/minecraft/:num<int>/backups/download", s.mcInstanceBackupDownload)
	app.Post("/api/minecraft/:num<int>/backups/delete", s.mcInstanceBackupDelete)
	app.Post("/api/minecraft/:num<int>/settings", s.mcInstanceSettingsSave)
	app.Post("/api/minecraft/:num<int>/mods/install", s.mcInstanceModsInstall)
	app.Post("/api/minecraft/:num<int>/mods/remove", s.mcInstanceModsRemove)
	app.Get("/api/minecraft/:num<int>/configs/file", s.mcInstanceConfigGet)
	app.Post("/api/minecraft/:num<int>/configs/save", s.mcInstanceConfigSave)
	app.Post("/api/minecraft/:num<int>/configs/delete", s.mcInstanceConfigDelete)
}

func (s *FiberServer) actor(c *fiber.Ctx) string {
	if v, ok := c.Locals("actor").(string); ok && v != "" {
		return v
	}
	return "local"
}

func (s *FiberServer) guard(action, okMsg string, fn func(context.Context) error) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if s.valheimRuntime == nil && s.k8s == nil {
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
		if s.mcRuntime == nil && s.mck8s == nil {
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
