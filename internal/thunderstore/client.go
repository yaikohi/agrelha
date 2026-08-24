// Package thunderstore queries the Thunderstore API to search Valheim mods and
// resolve a package's full dependency tree into mods.txt entries
// (namespace/name/version). BepInExPack is skipped — the server image provides
// it via BEPINEX=true (see valheim-mods.yaml).
package thunderstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	v1URL  string // community list, e.g. https://thunderstore.io/c/valheim/api/v1
	expURL string // https://thunderstore.io/api/experimental
	http   *http.Client
}

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

// Search filters the community package list by a case-insensitive substring.
// (The list is a few MB; a real impl would cache it — TODO in mod_cache.)
func (c *Client) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	var all []SearchResult
	if err := c.getJSON(ctx, c.v1URL+"/package/", &all); err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	var out []SearchResult
	for _, p := range all {
		if q == "" || strings.Contains(strings.ToLower(p.Name), q) || strings.Contains(strings.ToLower(p.Owner), q) {
			out = append(out, p)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
