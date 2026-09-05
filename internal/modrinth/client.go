// Package modrinth provides a client for the Modrinth v2 REST API (https://api.modrinth.com/v2).
// Used by Agrelha to search, inspect, and resolve dependencies for Minecraft NeoForge mods.
package modrinth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultBaseURL   = "https://api.modrinth.com/v2"
	DefaultUserAgent = "ykhi/agrelha/0.9.0 (ykhi@proton.me)"
)

type Client struct {
	baseURL   string
	userAgent string
	http      *http.Client
}

func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		userAgent: DefaultUserAgent,
		http:      &http.Client{Timeout: 15 * time.Second},
	}
}

type SearchHit struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Categories  []string `json:"categories"`
	Author      string   `json:"author"`
	IconURL     string   `json:"icon_url"`
	ProjectID   string   `json:"project_id"`
	Downloads   int      `json:"downloads"`
	Follows     int      `json:"follows"`
	Versions    []string `json:"versions"`
}

type SearchResponse struct {
	Hits      []SearchHit `json:"hits"`
	Offset    int         `json:"offset"`
	Limit     int         `json:"limit"`
	TotalHits int         `json:"total_hits"`
}

type Project struct {
	ID           string   `json:"id"`
	Slug         string   `json:"slug"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Body         string   `json:"body"`
	IconURL      string   `json:"icon_url"`
	SourceURL    string   `json:"source_url"`
	IssuesURL    string   `json:"issues_url"`
	WikiURL      string   `json:"wiki_url"`
	Categories   []string `json:"categories"`
	Loaders      []string `json:"loaders"`
	GameVersions []string `json:"game_versions"`
	ClientSide   string   `json:"client_side"` // "required", "optional", "unsupported"
	ServerSide   string   `json:"server_side"` // "required", "optional", "unsupported"
	Downloads    int      `json:"downloads"`
}

type VersionDependency struct {
	VersionID      *string `json:"version_id"`
	ProjectID      *string `json:"project_id"`
	FileName       *string `json:"file_name"`
	DependencyType string  `json:"dependency_type"` // "required", "optional", "incompatible", "embedded"
}

type VersionFile struct {
	Hashes   map[string]string `json:"hashes"` // "sha1", "sha512"
	URL      string            `json:"url"`
	FileName string            `json:"filename"`
	Primary  bool              `json:"primary"`
	Size     int64             `json:"size"`
}

type Version struct {
	ID           string              `json:"id"`
	ProjectID    string              `json:"project_id"`
	AuthorID     string              `json:"author_id"`
	Name         string              `json:"name"`
	VersionNum   string              `json:"version_number"`
	GameVersions []string            `json:"game_versions"`
	Loaders      []string            `json:"loaders"`
	Files        []VersionFile       `json:"files"`
	Dependencies []VersionDependency `json:"dependencies"`
	DatePub      time.Time           `json:"date_published"`
}

func (c *Client) getJSON(ctx context.Context, endpoint string, v any) error {
	reqURL := c.baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned status %s", reqURL, resp.Status)
	}

	return json.NewDecoder(resp.Body).Decode(v)
}

// Search queries Modrinth for NeoForge mods compatible with mcVersion.
func (c *Client) Search(ctx context.Context, query, mcVersion string, limit, offset int) (*SearchResponse, error) {
	if limit <= 0 {
		limit = 20
	}
	facets := `[["categories:neoforge"],["project_type:mod"]]`
	if mcVersion != "" {
		facets = fmt.Sprintf(`[["categories:neoforge"],["project_type:mod"],["versions:%s"]]`, mcVersion)
	}

	params := url.Values{}
	params.Set("query", query)
	params.Set("facets", facets)
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("offset", fmt.Sprintf("%d", offset))
	params.Set("index", "downloads")

	var res SearchResponse
	if err := c.getJSON(ctx, "/search?"+params.Encode(), &res); err != nil {
		return nil, fmt.Errorf("modrinth search: %w", err)
	}
	return &res, nil
}

// GetProject returns project metadata by slug or ID.
func (c *Client) GetProject(ctx context.Context, idOrSlug string) (*Project, error) {
	var p Project
	if err := c.getJSON(ctx, "/project/"+url.PathEscape(idOrSlug), &p); err != nil {
		return nil, fmt.Errorf("modrinth get project %q: %w", idOrSlug, err)
	}
	return &p, nil
}

// GetProjectVersions returns versions for a project filtered by neoforge and optionally mcVersion.
func (c *Client) GetProjectVersions(ctx context.Context, idOrSlug, mcVersion string) ([]Version, error) {
	params := url.Values{}
	params.Set("loaders", `["neoforge"]`)
	if mcVersion != "" {
		params.Set("game_versions", fmt.Sprintf(`["%s"]`, mcVersion))
	}

	var versions []Version
	endpoint := fmt.Sprintf("/project/%s/version?%s", url.PathEscape(idOrSlug), params.Encode())
	if err := c.getJSON(ctx, endpoint, &versions); err != nil {
		return nil, fmt.Errorf("modrinth get project versions %q: %w", idOrSlug, err)
	}
	return versions, nil
}

// GetVersion returns details for a specific version ID.
func (c *Client) GetVersion(ctx context.Context, versionID string) (*Version, error) {
	var v Version
	if err := c.getJSON(ctx, "/version/"+url.PathEscape(versionID), &v); err != nil {
		return nil, fmt.Errorf("modrinth get version %q: %w", versionID, err)
	}
	return &v, nil
}

// ResolveRequiredDependencies recursively finds all required project slugs/IDs for a mod on a given mcVersion.
func (c *Client) ResolveRequiredDependencies(ctx context.Context, idOrSlug, mcVersion string) ([]string, error) {
	visited := make(map[string]bool)
	var resolved []string

	var traverse func(string) error
	traverse = func(curr string) error {
		if visited[curr] {
			return nil
		}
		visited[curr] = true

		versions, err := c.GetProjectVersions(ctx, curr, mcVersion)
		if err != nil || len(versions) == 0 {
			// If no specific neoforge version for this exact MC version, try without mcVersion filter as fallback
			versions, err = c.GetProjectVersions(ctx, curr, "")
			if err != nil || len(versions) == 0 {
				return nil
			}
		}

		latest := versions[0]
		for _, dep := range latest.Dependencies {
			if dep.DependencyType == "required" && dep.ProjectID != nil {
				depProj, err := c.GetProject(ctx, *dep.ProjectID)
				if err != nil {
					continue
				}
				if !visited[depProj.Slug] {
					resolved = append(resolved, depProj.Slug)
					if err := traverse(depProj.Slug); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}

	if err := traverse(idOrSlug); err != nil {
		return nil, err
	}
	return resolved, nil
}
