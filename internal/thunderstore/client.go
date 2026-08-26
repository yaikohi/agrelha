package thunderstore

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type Client struct {
	v1URL  string
	expURL string
	http   *http.Client

	OnRefresh func([]SearchResult)

	mu        sync.Mutex
	index     []SearchResult
	byName    map[string]SearchResult
	indexedAt time.Time
}

const (
	indexTTL     = 6 * time.Hour
	indexTimeout = 3 * time.Minute
)

func New(v1URL string) *Client {
	return &Client{
		v1URL:  strings.TrimRight(v1URL, "/"),
		expURL: "https://thunderstore.io/api/experimental",
		http:   &http.Client{Timeout: 15 * time.Second},
	}
}

type expPackage struct {
	Namespace string     `json:"namespace"`
	Name      string     `json:"name"`
	Latest    expVersion `json:"latest"`
}
type expVersion struct {
	VersionNumber string   `json:"version_number"`
	Dependencies  []string `json:"dependencies"`
}

func (c *Client) getJSON(ctx context.Context, url string, v any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s -> %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func entry(dep string) (line string, skip bool) {
	parts := strings.Split(dep, "-")
	if len(parts) < 3 {
		return "", true
	}
	ns, ver := parts[0], parts[len(parts)-1]
	name := strings.Join(parts[1:len(parts)-1], "-")
	if strings.HasPrefix(strings.ToLower(name), "bepinexpack") {
		return "", true
	}
	return fmt.Sprintf("%s/%s/%s", ns, name, ver), false
}

func (c *Client) ResolveTree(ctx context.Context, ns, name string) ([]string, error) {
	var out []string
	visited := map[string]bool{}

	var root expPackage
	if err := c.getJSON(ctx, fmt.Sprintf("%s/package/%s/%s/", c.expURL, ns, name), &root); err != nil {
		return nil, err
	}
	type ref struct{ ns, name, ver string }
	queue := []ref{{ns, name, root.Latest.VersionNumber}}

	for len(queue) > 0 {
		r := queue[0]
		queue = queue[1:]
		key := r.ns + "/" + r.name
		if visited[key] {
			continue
		}
		visited[key] = true

		line, skip := entry(fmt.Sprintf("%s-%s-%s", r.ns, r.name, r.ver))
		if skip {
			continue
		}
		out = append(out, line)

		var ver expVersion
		u := fmt.Sprintf("%s/package/%s/%s/%s/", c.expURL, r.ns, r.name, r.ver)
		if err := c.getJSON(ctx, u, &ver); err != nil {
			return nil, fmt.Errorf("deps of %s: %w", line, err)
		}
		for _, dep := range ver.Dependencies {
			parts := strings.Split(dep, "-")
			if len(parts) < 3 {
				continue
			}
			dn := strings.Join(parts[1:len(parts)-1], "-")
			if !visited[parts[0]+"/"+dn] {
				queue = append(queue, ref{parts[0], dn, parts[len(parts)-1]})
			}
		}
	}
	return out, nil
}

func (c *Client) LatestVersion(ctx context.Context, ns, name string) (version string, deps []string, err error) {
	var p expPackage
	if err := c.getJSON(ctx, fmt.Sprintf("%s/package/%s/%s/", c.expURL, ns, name), &p); err != nil {
		return "", nil, err
	}
	return p.Latest.VersionNumber, p.Latest.Dependencies, nil
}

func (c *Client) Readme(ctx context.Context, ns, name, version string) (string, error) {
	var r struct {
		Markdown string `json:"markdown"`
	}
	u := fmt.Sprintf("%s/package/%s/%s/%s/readme/", c.expURL, ns, name, version)
	if err := c.getJSON(ctx, u, &r); err != nil {
		return "", err
	}
	return r.Markdown, nil
}

type SearchResult struct {
	Owner        string
	Name         string
	FullURL      string
	Description  string
	Icon         string
	Version      string
	Downloads    int64
	IsDeprecated bool
	UpdatedAt    time.Time
}

func (r SearchResult) FullName() string { return r.Owner + "/" + r.Name }

type rawPackage struct {
	Name         string `json:"name"`
	Owner        string `json:"owner"`
	PackageURL   string `json:"package_url"`
	IsDeprecated bool   `json:"is_deprecated"`
	Downloads    int64  `json:"total_downloads"`
	DateUpdated  string `json:"date_updated"`
	Versions     []struct {
		Description   string `json:"description"`
		Icon          string `json:"icon"`
		VersionNumber string `json:"version_number"`
	} `json:"versions"`
}

func (c *Client) Ready() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.index != nil
}

func (c *Client) Get(fullName string) (SearchResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byName[fullName]
	return e, ok
}

func (c *Client) Preload(idx []SearchResult, at time.Time) {
	c.setIndex(idx, at)
}

func (c *Client) setIndex(idx []SearchResult, at time.Time) {
	byName := make(map[string]SearchResult, len(idx))
	for _, e := range idx {
		byName[e.FullName()] = e
	}
	c.mu.Lock()
	c.index, c.byName, c.indexedAt = idx, byName, at
	c.mu.Unlock()
}

func (c *Client) WarmLoop(ctx context.Context) {
	for {
		c.mu.Lock()
		have := c.index != nil
		age := time.Since(c.indexedAt)
		c.mu.Unlock()

		if !have || age >= indexTTL {
			if err := c.warm(ctx); err != nil {
				log.Printf("thunderstore: index build failed: %v", err)
			} else {
				c.mu.Lock()
				n := len(c.index)
				c.mu.Unlock()
				log.Printf("thunderstore: search index built (%d packages)", n)
			}
		}

		c.mu.Lock()
		sleep := indexTTL - time.Since(c.indexedAt)
		c.mu.Unlock()
		if sleep < time.Minute {
			sleep = time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(sleep):
		}
	}
}

func (c *Client) warm(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, indexTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.v1URL+"/package/", nil)
	resp, err := (&http.Client{Timeout: indexTimeout}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s -> %s", c.v1URL+"/package/", resp.Status)
	}

	dec := json.NewDecoder(resp.Body)
	if _, err := dec.Token(); err != nil {
		return err
	}
	var idx []SearchResult
	for dec.More() {
		var p rawPackage
		if err := dec.Decode(&p); err != nil {
			return err
		}
		r := SearchResult{
			Owner:        p.Owner,
			Name:         p.Name,
			FullURL:      p.PackageURL,
			Downloads:    p.Downloads,
			IsDeprecated: p.IsDeprecated,
		}
		if len(p.Versions) > 0 {
			r.Description = p.Versions[0].Description
			r.Icon = p.Versions[0].Icon
			r.Version = p.Versions[0].VersionNumber
		}
		if t, err := time.Parse(time.RFC3339, p.DateUpdated); err == nil {
			r.UpdatedAt = t
		}
		idx = append(idx, r)
	}
	sort.Slice(idx, func(i, j int) bool { return idx[i].Downloads > idx[j].Downloads })

	c.setIndex(idx, time.Now())
	if c.OnRefresh != nil {
		c.OnRefresh(idx)
	}
	return nil
}

func (c *Client) Search(_ context.Context, query string, limit int) ([]SearchResult, error) {
	c.mu.Lock()
	idx := c.index
	c.mu.Unlock()

	q := strings.ToLower(query)
	var out []SearchResult
	for _, p := range idx {
		if q == "" || strings.Contains(strings.ToLower(p.Name), q) || strings.Contains(strings.ToLower(p.Owner), q) {
			out = append(out, p)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
