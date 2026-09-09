package wizard

import (
	"context"

	mccontent "agrelha/internal/app/content"
	"agrelha/internal/app/instances"
	"github.com/gofiber/fiber/v2"
)

// ModpackHit represents a search result from an external modpack index.
type ModpackHit struct {
	ID            int
	Name          string
	Summary       string
	ThumbnailURL  string
	DownloadCount int64
	RefURL        string
}

// ModHit represents a single mod search result from an external repository.
type ModHit struct {
	Slug        string
	Title       string
	Description string
	IconURL     string
}

// Config defines the narrow delivery dependencies for the Minecraft provisioning wizard.
type Config struct {
	MCInstances      *instances.InstanceManager
	Actor            func(*fiber.Ctx) string
	VersionReleases  func(ctx context.Context, limit int) []string
	SearchModpacks   func(ctx context.Context, query string) ([]ModpackHit, error)
	ResolvePackRef   func(ctx context.Context, packRef string) string
	VerifyPackLoader func(ctx context.Context, packID int, packName, requestedLoader string) string
	SearchMods       func(ctx context.Context, query, mcVersion string) ([]ModHit, error)
	CheckCartCompat  func(ctx context.Context, slugs []string, mcVersion string) mccontent.CartCompatibility
}

type Handler struct {
	cfg Config
}

func New(cfg Config) *Handler {
	if cfg.Actor == nil {
		cfg.Actor = func(c *fiber.Ctx) string {
			if a, ok := c.Locals("actor").(string); ok && a != "" {
				return a
			}
			return "local"
		}
	}
	return &Handler{cfg: cfg}
}

// Register mounts the provisioning routes.
func (h *Handler) Register(router fiber.Router) {
	router.Get("/minecraft/create", h.MCWizardPage)
	router.Get("/minecraft/provisioning/:num", h.MCProvisioningPage)
	router.Get("/api/minecraft/provisioning/:num/stream", h.MCProvisioningStream)
	router.Get("/api/minecraft/wizard/modpacks/search", h.MCWizardModpacksSearch)
	router.Post("/api/minecraft/wizard/modpacks/search", h.MCWizardModpacksSearch)
	router.Get("/api/minecraft/wizard/mods/search", h.MCWizardModsSearch)
	router.Post("/api/minecraft/wizard/mods/search", h.MCWizardModsSearch)
	router.Post("/api/minecraft/wizard/cart/check", h.MCWizardCartCheck)
	router.Post("/api/minecraft/wizard/create", h.MCWizardCreate)
	router.Post("/api/minecraft/wizard/import", h.MCWizardImport)
}
