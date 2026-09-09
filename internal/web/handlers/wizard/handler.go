package wizard

import (
	"github.com/gofiber/fiber/v2"

	"agrelha/internal/infra/content/mcversions"
	"agrelha/internal/infra/content/modpackindex"
	"agrelha/internal/infra/content/modrinth"
	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/store"
	"agrelha/internal/minecraft"
)

// Config is deliberately narrower than instances.Config: these seven fields are
// everything the provisioning flow touches.
type Config struct {
	Store       *store.Store
	MCK8s       *k8s.Client
	MCInstances *minecraft.InstanceManager
	MCV         *mcversions.Client
	MPI         *modpackindex.Client
	MR          *modrinth.Client
	Actor       func(*fiber.Ctx) string
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
