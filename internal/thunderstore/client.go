// Package thunderstore queries the Thunderstore API to search Valheim mods and
// resolve a package's full dependency tree into mods.txt entries
// (namespace/name/version). BepInExPack is skipped — the server image provides
// it via BEPINEX=true (see valheim-mods.yaml).
package thunderstore

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Client struct {
	v1URL  string       // community list, e.g. https://thunderstore.io/c/valheim/api/v1
	expURL string       // https://thunderstore.io/api/experimental
	http   *http.Client // short timeout, for small package/version lookups

	mu        sync.Mutex // guards the search index
	index     []SearchResult
	indexedAt time.Time
}

const (
	indexTTL     = 6 * time.Hour   // how often to rebuild the search index
	indexTimeout = 3 * time.Minute // the community list is ~160MB; give the stream room
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
	Dependencies  []string `json:"dependencies"` // "Namespace-Name-Version"
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

// entry converts a "Namespace-Name-Version" dependency string to a mods.txt
// "namespace/name/version" entry. Returns skip=true for BepInExPack.
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

// ResolveTree returns the target package plus its full transitive dependency set
// as deduped mods.txt entries. visited is keyed by namespace/name (first version
// wins on conflict).
func (c *Client) ResolveTree(ctx context.Context, ns, name string) ([]string, error) {
	var out []string
	visited := map[string]bool{}

	// seed with the target's latest version
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

// SearchResult is a trimmed community-list package for the browse UI.
type SearchResult struct {
	Owner   string `json:"owner"`
	Name    string `json:"name"`
	FullURL string `json:"package_url"`
}

// Ready reports whether the search index has been built at least once.
func (c *Client) Ready() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.index != nil
}

// WarmLoop builds the search index immediately and rebuilds it every indexTTL.
// Run it in a background goroutine at startup.
func (c *Client) WarmLoop(ctx context.Context) {
	for {
		if err := c.warm(ctx); err != nil {
			log.Printf("thunderstore: index build failed: %v", err)
		} else {
			c.mu.Lock()
			n := len(c.index)
			c.mu.Unlock()
			log.Printf("thunderstore: search index built (%d packages)", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(indexTTL):
		}
	}
}

// warm streams the ~160MB community list and keeps only owner/name/url per
// package. Streaming (element-by-element) keeps peak memory tiny — we never hold
// the whole body or all fields.
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
	if _, err := dec.Token(); err != nil { // opening '['
		return err
	}
	var idx []SearchResult
	for dec.More() {
		var p SearchResult // unknown fields are discarded per element
		if err := dec.Decode(&p); err != nil {
			return err
		}
		idx = append(idx, p)
	}
	c.mu.Lock()
	c.index, c.indexedAt = idx, time.Now()
	c.mu.Unlock()
	return nil
}

// Search filters the in-memory index by a case-insensitive substring. Returns
// nothing until the index is ready (see Ready()).
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
