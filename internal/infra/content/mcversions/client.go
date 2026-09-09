package mcversions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultManifestURL = "https://launchermeta.mojang.com/mc/game/version_manifest_v2.json"
	FallbackLatest     = "1.21.1"
	DefaultCacheTTL    = 6 * time.Hour
)

type manifest struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"versions"`
}

type Client struct {
	url     string
	ttl     time.Duration
	http    *http.Client
	mu      sync.Mutex
	cache   *manifest
	fetched time.Time
}

func New(url string) *Client {
	if url == "" {
		url = DefaultManifestURL
	}
	return &Client{
		url:  url,
		ttl:  DefaultCacheTTL,
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) load(ctx context.Context) (*manifest, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cache != nil && time.Since(c.fetched) < c.ttl {
		return c.cache, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if c.cache != nil {
			return c.cache, nil
		}
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if c.cache != nil {
			return c.cache, nil
		}
		return nil, fmt.Errorf("version manifest returned %s", resp.Status)
	}

	var m manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		if c.cache != nil {
			return c.cache, nil
		}
		return nil, err
	}

	c.cache = &m
	c.fetched = time.Now()
	return c.cache, nil
}

func (c *Client) Latest(ctx context.Context) string {
	m, err := c.load(ctx)
	if err != nil || m.Latest.Release == "" {
		return FallbackLatest
	}
	return m.Latest.Release
}

func (c *Client) Releases(ctx context.Context, limit int) []string {
	m, err := c.load(ctx)
	if err != nil {
		return []string{FallbackLatest}
	}
	out := make([]string, 0, len(m.Versions))
	for _, v := range m.Versions {
		if v.Type != "release" {
			continue
		}
		out = append(out, v.ID)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	if len(out) == 0 {
		return []string{FallbackLatest}
	}
	return out
}

func (c *Client) IsRelease(ctx context.Context, version string) bool {
	m, err := c.load(ctx)
	if err != nil {
		return false
	}
	for _, v := range m.Versions {
		if v.ID == version && v.Type == "release" {
			return true
		}
	}
	return false
}

func Compare(a, b string) int {
	as, bs := splitVersion(a), splitVersion(b)
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func splitVersion(v string) []int {
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

func IsModern(v string) bool {
	s := splitVersion(v)
	return len(s) > 0 && s[0] >= 26
}
