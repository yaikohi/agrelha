package access

import (
	mcaccess "agrelha/internal/app/access"
	"agrelha/internal/app/admins"
	"agrelha/internal/ports"

	"github.com/gofiber/fiber/v2"
)

// Config defines dependencies for access management HTTP handlers.
type Config struct {
	Admins         *admins.Manager
	MCAccess       *mcaccess.AccessManager
	History        ports.HistoryReader
	Players        ports.PlayerReader
	StateStore     ports.StateStore
	Actor          func(*fiber.Ctx) string
	ApplyAfterSync func(cmName, key string, want func(string) bool)
}

// Handler serves endpoints for game operator management and whitelisting.
type Handler struct {
	cfg Config
}

// New creates a new access Handler.
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

// Register mounts all access routes onto the provided Fiber router.
func (h *Handler) Register(router fiber.Router) {
	router.Get("/admins", h.AdminsPage)
	router.Post("/admins/grant", h.AdminsGrant)
	router.Post("/admins/revoke", h.AdminsRevoke)
	router.Get("/history", h.HistoryPage)

	router.Get("/minecraft/access", h.MCAccessPage)
	router.Post("/api/minecraft/access/op", h.MCAccessGrantOp)
	router.Post("/api/minecraft/access/deop", h.MCAccessRevokeOp)
	router.Post("/api/minecraft/access/whitelist/add", h.MCAccessAddWhitelist)
	router.Post("/api/minecraft/access/whitelist/remove", h.MCAccessRemoveWhitelist)
	router.Post("/api/minecraft/access/whitelist/toggle", h.MCAccessWhitelistToggle)
}
