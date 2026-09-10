package console

import (
	"context"
	"log/slog"
	"time"

	"agrelha/internal/app/instances"
	"agrelha/internal/ports"
	"agrelha/internal/web/metrics"
	"agrelha/internal/web/shared"

	"github.com/gofiber/fiber/v2"
)

// Config defines dependencies for console, log tailing, and server lifecycle handlers.
type Config struct {
	ValheimRuntime   ports.Runtime
	ValheimRef       ports.ServerRef
	ValheimInstances *instances.InstanceManager
	MCRuntime        ports.Runtime
	MCRef            ports.ServerRef
	MCInstances      *instances.InstanceManager
	Audit            ports.AuditRecorder
	Event            ports.EventRecorder
	Auth             ports.Auth
	Actor            func(*fiber.Ctx) string
}

// Handler handles console viewing, log streaming, and imperative actions.
type Handler struct {
	cfg Config
}

// New creates a new console Handler.
func New(cfg Config) *Handler {
	if cfg.Actor == nil {
		cfg.Actor = func(c *fiber.Ctx) string {
			if a, ok := c.Locals("actor").(string); ok && a != "" {
				return a
			}
			return "-"
		}
	}
	return &Handler{cfg: cfg}
}

// Register mounts console routes onto the provided Fiber router.
func (h *Handler) Register(router fiber.Router) {
	router.Get("/valheim", h.ValheimConsole)
	router.Get("/valheim/console", h.ValheimConsole)
	router.Get("/sse/logs", h.SSELogs)
	router.Post("/server/restart", h.ServerRestart)
	router.Post("/server/update", h.ServerUpdate)
	router.Post("/server/stop", h.ServerStop)
	router.Post("/server/start", h.ServerStart)

	router.Get("/api/valheim/:num<int>/logs", h.ValheimLogsStream)
	router.Get("/api/valheim/:num<int>/logs/stream", h.ValheimLogsStream)

	router.Post("/api/minecraft/:num<int>/rcon", h.MCRconCommand)
	router.Get("/api/minecraft/:num<int>/logs", h.MCLogsStream)
	router.Get("/api/minecraft/:num<int>/logs/stream", h.MCLogsStream)
	router.Post("/minecraft/server/restart", h.MCServerRestart)
	router.Post("/minecraft/server/stop", h.MCServerStop)
	router.Post("/minecraft/server/start", h.MCServerStart)
}

func (h *Handler) MCServerRestart(c *fiber.Ctx) error {
	return h.guardMC("mc-restart", "Minecraft restart triggered — server is rolling.", func(ctx context.Context) error {
		return h.cfg.MCRuntime.Restart(ctx, h.cfg.MCRef)
	})(c)
}

func (h *Handler) MCServerStop(c *fiber.Ctx) error {
	return h.guardMC("mc-stop", "Stopping Minecraft server — scaling to 0.", func(ctx context.Context) error {
		return h.cfg.MCRuntime.Stop(ctx, h.cfg.MCRef)
	})(c)
}

func (h *Handler) MCServerStart(c *fiber.Ctx) error {
	return h.guardMC("mc-start", "Starting Minecraft server — scaling to 1.", func(ctx context.Context) error {
		return h.cfg.MCRuntime.Start(ctx, h.cfg.MCRef)
	})(c)
}

func (h *Handler) guardMC(action, okMsg string, fn func(context.Context) error) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if h.cfg.MCRuntime == nil {
			metrics.ControlActions.WithLabelValues(action, "disabled").Inc()
			return shared.SSEToast(c, "err", "Minecraft imperative plane disabled — no cluster access.", nil)
		}
		ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			metrics.ControlActions.WithLabelValues(action, "error").Inc()
			slog.Error("minecraft control action failed", "action", action, "err", err)
			return shared.SSEToast(c, "err", action+" failed: "+err.Error(), nil)
		}
		metrics.ControlActions.WithLabelValues(action, "ok").Inc()
		if h.cfg.Audit != nil {
			_ = h.cfg.Audit.RecordAudit(h.cfg.Actor(c), action, "")
		}
		if h.cfg.Event != nil {
			_ = h.cfg.Event.RecordEvent(action, h.cfg.Actor(c))
		}
		return shared.SSEToast(c, "ok", okMsg, nil)
	}
}

func (h *Handler) guard(action, okMsg string, fn func(context.Context) error) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if h.cfg.ValheimRuntime == nil {
			metrics.ControlActions.WithLabelValues(action, "disabled").Inc()
			return shared.SSEToast(c, "err", "Imperative plane disabled — no cluster access.", nil)
		}
		ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			metrics.ControlActions.WithLabelValues(action, "error").Inc()
			return shared.SSEToast(c, "err", action+" failed: "+err.Error(), nil)
		}
		metrics.ControlActions.WithLabelValues(action, "ok").Inc()
		if h.cfg.Audit != nil {
			_ = h.cfg.Audit.RecordAudit(h.cfg.Actor(c), action, "")
		}
		if h.cfg.Event != nil {
			_ = h.cfg.Event.RecordEvent(action, h.cfg.Actor(c))
		}
		return shared.SSEToast(c, "ok", okMsg, nil)
	}
}
