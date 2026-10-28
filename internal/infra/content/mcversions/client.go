package mcversions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"agrelha/internal/domain"
)

const (
	DefaultManifestURL = "https://launchermeta.mojang.com/mc/game/version_manifest_v2.json"
	FallbackLatest     = "1.21.1"

	// Mojang dropped the leading "1." in 2026; anything at or past this is on
	// the new scheme.
	firstModernRelease = "26.0"
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

func Compare(a, b string) int { return domain.CompareMCVersions(a, b) }

func IsModern(v string) bool {
	return domain.CompareMCVersions(v, firstModernRelease) >= 0
}
