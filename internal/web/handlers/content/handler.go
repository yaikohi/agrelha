package content

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/ports"
)

// Config configures dependencies for the content management handler.
type Config struct {
	ModConfigsPath string
	StateStore     ports.StateStore
	Audit          ports.AuditRecorder
	ReadmeCache    ports.ReadmeCache
	ValheimGame    ports.Game
	TS             ports.PackageCatalog
	ConfigData     func(context.Context) (map[string]string, error)
	Actor          func(*fiber.Ctx) string
	ApplyAfterSync func(cmName, key string, check func(string) bool)
}

// Handler serves routes and operations for mod metadata, configs, and exports.
type Handler struct {
	cfg Config
}

// New constructs a new content Handler.
func New(cfg Config) *Handler {
	if cfg.Actor == nil {
		cfg.Actor = func(c *fiber.Ctx) string {
			if a, ok := c.Locals("actor").(string); ok && a != "" {
				return a
			}
			return "local"
		}
	}
	if cfg.ApplyAfterSync == nil {
		cfg.ApplyAfterSync = func(string, string, func(string) bool) {}
	}
	return &Handler{cfg: cfg}
}

// Register mounts content routes onto the provided Fiber router.
func (h *Handler) Register(router fiber.Router) {
	h.RegisterPublic(router)
	h.RegisterProtected(router)
}

// RegisterPublic mounts unauthenticated content routes (e.g. image proxy and modpack download).
func (h *Handler) RegisterPublic(router fiber.Router) {
	router.Get("/img", h.ImageProxy)
}

// RegisterProtected mounts authenticated content management routes.
func (h *Handler) RegisterProtected(router fiber.Router) {
	router.Get("/mods/:namespace/:name", h.ModDetail)
	router.Get("/configs", h.ConfigsPage)
	router.Get("/configs/new", h.ConfigNew)
	router.Get("/configs/edit", h.ConfigEdit)
	router.Post("/configs/save", h.ConfigSave)
	router.Post("/configs/delete", h.ConfigDelete)
}

const maxImageBytes = 12 << 20

var imgHTTP = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		if req.URL.Scheme != "https" || !PublicHost(req.URL.Hostname()) {
			return fmt.Errorf("blocked redirect target")
		}
		return nil
	},
}

// ImageProxy proxies remote HTTPS images safely with SSRF protection and limit reader.
func (h *Handler) ImageProxy(c *fiber.Ctx) error {
	raw := c.Query("u")
	if raw == "" {
		return fiber.NewError(fiber.StatusBadRequest, "missing u")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fiber.NewError(fiber.StatusBadRequest, "url must be absolute https")
	}
	if !PublicHost(u.Hostname()) {
		return fiber.NewError(fiber.StatusForbidden, "host not allowed")
	}

	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("User-Agent", "agrelha-image-proxy")
	req.Header.Set("Accept", "image/*")

	resp, err := imgHTTP.Do(req)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "fetch failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fiber.NewError(fiber.StatusBadGateway, "upstream "+resp.Status)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		return fiber.NewError(fiber.StatusUnsupportedMediaType, "not an image")
	}

	c.Set(fiber.HeaderContentType, ct)
	c.Set(fiber.HeaderCacheControl, "public, max-age=604800, immutable")
	c.Set("X-Content-Type-Options", "nosniff")
	return c.SendStream(io.LimitReader(resp.Body, maxImageBytes))
}

// PublicHost verifies that a given hostname does not resolve to private or loopback IP ranges.
func PublicHost(host string) bool {
	if host == "" {
		return false
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
			ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return false
		}
	}
	return true
}

func (h *Handler) configData(ctx context.Context) (map[string]string, error) {
	if h.cfg.ConfigData != nil {
		return h.cfg.ConfigData(ctx)
	}
	if h.cfg.StateStore != nil && h.cfg.ModConfigsPath != "" {
		doc, err := h.cfg.StateStore.Get(ctx, h.cfg.ModConfigsPath)
		if err != nil {
			return nil, err
		}
		return doc.Data, nil
	}
	return nil, nil
}
