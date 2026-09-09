package console

import (
	"context"
	"time"

	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/store"
	"agrelha/internal/minecraft"
	"agrelha/internal/ports"
	"agrelha/internal/web/metrics"
	"agrelha/internal/web/shared"

	"github.com/gofiber/fiber/v2"
)

// Config defines dependencies for console, log tailing, and server lifecycle handlers.
type Config struct {
	K8s         *k8s.Client
	MCK8s       *k8s.Client
	MCInstances *minecraft.InstanceManager
	MCRconPool  *minecraft.RconPool
	Store       *store.Store
	Auth        ports.Auth
	Actor       func(*fiber.Ctx) string
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
	router.Get("/sse/logs", h.SSELogs)
	router.Post("/server/restart", h.ServerRestart)
	router.Post("/server/update", h.ServerUpdate)
	router.Post("/server/stop", h.ServerStop)
	router.Post("/server/start", h.ServerStart)

	router.Post("/api/minecraft/:num<int>/rcon", h.MCRconCommand)
	router.Get("/api/minecraft/:num<int>/logs", h.MCLogsStream)
}

func (h *Handler) guard(action, okMsg string, fn func(context.Context) error) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if h.cfg.K8s == nil {
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
		if h.cfg.Store != nil {
			_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), action, "")
			_ = h.cfg.Store.RecordEvent(action, h.cfg.Actor(c))
		}
		return shared.SSEToast(c, "ok", okMsg, nil)
	}
}
