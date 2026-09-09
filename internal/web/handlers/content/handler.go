package content

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/mods"
	"agrelha/internal/infra/content/thunderstore"
	"agrelha/internal/infra/gitops"
	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/store"
	"agrelha/internal/platform/config"
	"agrelha/internal/ports"
)

// Config configures dependencies for the content management handler.
type Config struct {
	Cfg            *config.Config
	Store          *store.Store
	K8s            *k8s.Client
	ValheimGame    ports.Game
	Mods           *mods.Manager
	Git            *gitops.Committer
	TS             *thunderstore.Client
	Actor          func(*fiber.Ctx) string
	ApplyAfterSync func(cmName, key string, check func(string) bool)
	PendingActive  func(context.Context) bool
	SetPending     func(entries []string)
}

// Handler serves routes and operations for mods, configs, updates, and exports.
type Handler struct {
	cfg Config

	pendMu  sync.Mutex
	pendSet map[string]bool
	pendAt  time.Time
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
	router.Get("/img", h.ImageProxy)
	router.Get("/mods/export", h.ModpackExport)
	router.Get("/mods", h.ModsPage)
	router.Get("/mods/:namespace/:name", h.ModDetail)
	router.Post("/mods/install", h.ModsInstall)
	router.Post("/mods/remove", h.ModsRemove)
	router.Post("/mods/update/all", h.ModsUpdateAll)
	router.Post("/mods/update/selected", h.ModsUpdateSelected)
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

// RowsToResults converts store ModIndexRows to thunderstore SearchResults.
func RowsToResults(rows []store.ModIndexRow) []thunderstore.SearchResult {
	out := make([]thunderstore.SearchResult, 0, len(rows))
	for _, r := range rows {
		out = append(out, thunderstore.SearchResult{
			Owner:        r.Owner,
			Name:         r.Name,
			FullURL:      r.PackageURL,
			Description:  r.Description,
			Icon:         r.Icon,
			Version:      r.Version,
			Downloads:    r.Downloads,
			IsDeprecated: r.IsDeprecated,
			UpdatedAt:    r.UpdatedAt,
		})
	}
	return out
}

// ResultsToRows converts thunderstore SearchResults to store ModIndexRows.
func ResultsToRows(idx []thunderstore.SearchResult) []store.ModIndexRow {
	out := make([]store.ModIndexRow, 0, len(idx))
	for _, r := range idx {
		out = append(out, store.ModIndexRow{
			FullName:     r.FullName(),
			Namespace:    r.Owner,
			Name:         r.Name,
			Owner:        r.Owner,
			Version:      r.Version,
			Description:  r.Description,
			Icon:         r.Icon,
			PackageURL:   r.FullURL,
			Downloads:    r.Downloads,
			IsDeprecated: r.IsDeprecated,
			UpdatedAt:    r.UpdatedAt,
		})
	}
	return out
}
