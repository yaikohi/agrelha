package content

import (
	"agrelha/internal/web/shared"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/ports"
)

// Config configures dependencies for the content management handler.
type Config struct {
	StateStore     ports.StateStore
	Audit          ports.AuditRecorder
	ReadmeCache    ports.ReadmeCache
	ValheimGame    ports.Game
	TS             ports.PackageCatalog
	Actor          func(*fiber.Ctx) string
	ApplyAfterSync func(cmName, key string, check func(string) bool)
	ImageTransport http.RoundTripper
}

// Handler serves routes and operations for mod metadata, configs, and exports.
type Handler struct {
	cfg Config
}

// New constructs a new content Handler.
func New(cfg Config) *Handler {
	if cfg.Actor == nil {
		cfg.Actor = shared.Actor
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
}

const maxImageBytes = 12 << 20

func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

func safeDialControl(network, address string, c syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	if zoneIdx := strings.Index(host, "%"); zoneIdx != -1 {
		host = host[:zoneIdx]
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("invalid dialed IP address %q", address)
	}
	if isBlockedIP(ip) {
		return fmt.Errorf("dialed address %s is not a public IP", ip)
	}
	return nil
}

var imgTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   safeDialControl,
	}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}

var imgHTTP = &http.Client{
	Timeout:   15 * time.Second,
	Transport: imgTransport,
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

	resp, err := h.imageClient().Do(req)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "fetch failed")
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return fiber.NewError(fiber.StatusBadGateway, "upstream "+resp.Status)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		_ = resp.Body.Close()
		return fiber.NewError(fiber.StatusUnsupportedMediaType, "not an image")
	}

	c.Set(fiber.HeaderContentType, ct)
	c.Set(fiber.HeaderCacheControl, "public, max-age=604800, immutable")
	c.Set("X-Content-Type-Options", "nosniff")
	return c.SendStream(limitReadCloser{
		Reader: io.LimitReader(resp.Body, maxImageBytes),
		Closer: resp.Body,
	})
}

type limitReadCloser struct {
	io.Reader
	io.Closer
}

var lookupIP = net.LookupIP

// PublicHost verifies that a given hostname does not resolve to private or loopback IP ranges.
func PublicHost(host string) bool {
	if host == "" {
		return false
	}
	ips, err := lookupIP(host)
	if err != nil || len(ips) == 0 {
		return false
	}
	return !slices.ContainsFunc(ips, isBlockedIP)
}

func (h *Handler) imageClient() *http.Client {
	if h == nil || h.cfg.ImageTransport == nil {
		return imgHTTP
	}
	c := *imgHTTP
	c.Transport = h.cfg.ImageTransport
	return &c
}
