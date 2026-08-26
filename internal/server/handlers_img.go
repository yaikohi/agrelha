package server

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
)

const maxImageBytes = 12 << 20

var imgHTTP = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		if req.URL.Scheme != "https" || !publicHost(req.URL.Hostname()) {
			return fmt.Errorf("blocked redirect target")
		}
		return nil
	},
}

func (s *FiberServer) imageProxy(c *fiber.Ctx) error {
	raw := c.Query("u")
	if raw == "" {
		return fiber.NewError(fiber.StatusBadRequest, "missing u")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fiber.NewError(fiber.StatusBadRequest, "url must be absolute https")
	}
	if !publicHost(u.Hostname()) {
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

func publicHost(host string) bool {
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
