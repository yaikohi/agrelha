// Package modrinth provides a client for the Modrinth v2 REST API (https://api.modrinth.com/v2).
// Used by Agrelha to search, inspect, and resolve dependencies for Minecraft NeoForge mods.
package modrinth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

const (
	DefaultBaseURL   = "https://api.modrinth.com/v2"
	DefaultUserAgent = "ykhi/agrelha/0.23.13 (ykhi@proton.me)"
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

type Project = domain.ModProject
type VersionDependency = domain.VersionDependency
type VersionFile = domain.ModVersionFile
type Version = domain.ModVersion

var (
	sleep     = time.Sleep
	timeAfter = time.After
)

func (c *Client) getJSON(ctx context.Context, endpoint string, v any) error {
	reqURL := c.baseURL + endpoint
	const maxRetries = 4

	for attempt := range maxRetries {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempt < maxRetries-1 {
				sleep(time.Duration(attempt+1) * 250 * time.Millisecond)
				continue
			}
			return err
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			_ = resp.Body.Close()
			retryAfterSec := 1
			if val := resp.Header.Get("Retry-After"); val != "" {
				if s, err := strconv.Atoi(val); err == nil && s >= 0 {
					retryAfterSec = s
				}
			}
			waitDuration := time.Duration(retryAfterSec)*time.Second + time.Duration(attempt*250)*time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timeAfter(waitDuration):
				continue
			}
		}

		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			return fmt.Errorf("%s: %w", reqURL, ports.ErrPackageNotFound)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s returned status %s", reqURL, resp.Status)
		}

		return json.NewDecoder(resp.Body).Decode(v)
	}

	return fmt.Errorf("%s exceeded retry attempts due to rate limiting", reqURL)
}

// Search queries Modrinth for mods compatible with mcVersion and the specified loader (default "neoforge").
func (c *Client) Search(ctx context.Context, query, mcVersion, loader string, limit, offset int) (*SearchResponse, error) {
	if limit <= 0 {
		limit = 20
	}
	loader = strings.ToLower(strings.TrimSpace(loader))
	if loader == "" {
		loader = "neoforge"
	}
	facets := fmt.Sprintf(`[["categories:%s"],["project_type:mod"]]`, loader)
	if mcVersion != "" {
		facets = fmt.Sprintf(`[["categories:%s"],["project_type:mod"],["versions:%s"]]`, loader, mcVersion)
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

// GetProjects returns project metadata for multiple slugs or IDs in batch chunks of up to 100.
func (c *Client) GetProjects(ctx context.Context, idsOrSlugs []string) ([]Project, error) {
	if len(idsOrSlugs) == 0 {
		return nil, nil
	}

	var all []Project
	chunkSize := 100
	for i := 0; i < len(idsOrSlugs); i += chunkSize {
		end := min(i+chunkSize, len(idsOrSlugs))
		chunk := idsOrSlugs[i:end]
		idsJSON, err := json.Marshal(chunk)
		if err != nil {
			return nil, err
		}

		var projects []Project
		endpoint := fmt.Sprintf("/projects?ids=%s", url.QueryEscape(string(idsJSON)))
		if err := c.getJSON(ctx, endpoint, &projects); err != nil {
			return nil, fmt.Errorf("modrinth get projects batch: %w", err)
		}
		all = append(all, projects...)
	}

	return all, nil
}

// GetProjectVersions returns versions for a project filtered by loader (default "neoforge") and optionally mcVersion.
// For "neoforge", it queries both "neoforge" and "forge" loaders (critical for MC 1.20.x ecosystem compatibility).
func (c *Client) GetProjectVersions(ctx context.Context, idOrSlug, mcVersion, loader string) ([]Version, error) {
	loader = strings.ToLower(strings.TrimSpace(loader))
	if loader == "" {
		loader = "neoforge"
	}

	queryVersions := func(loaders []string, gameVer string) ([]Version, error) {
		loadersJSON, _ := json.Marshal(loaders)
		params := url.Values{}
		params.Set("loaders", string(loadersJSON))
		if gameVer != "" {
			params.Set("game_versions", fmt.Sprintf(`["%s"]`, gameVer))
		}

		var vers []Version
		endpoint := fmt.Sprintf("/project/%s/version?%s", url.PathEscape(idOrSlug), params.Encode())
		if err := c.getJSON(ctx, endpoint, &vers); err != nil {
			return nil, err
		}
		return vers, nil
	}

	// 1. Primary query: for neoforge, include forge as valid loader
	primaryLoaders := []string{loader}
	if loader == "neoforge" {
		primaryLoaders = []string{"neoforge", "forge"}
	}
	versions, err := queryVersions(primaryLoaders, mcVersion)
	if err == nil && len(versions) > 0 {
		return versions, nil
	}

	// 2. If no versions found for exact mcVersion with point release (e.g. 1.20.1), try base version (e.g. 1.20)
	if mcVersion != "" && strings.Count(mcVersion, ".") >= 2 {
		parts := strings.Split(mcVersion, ".")
		baseVer := parts[0] + "." + parts[1]
		if vers, err := queryVersions(primaryLoaders, baseVer); err == nil && len(vers) > 0 {
			return vers, nil
		}
	}

	// 3. Fallback: query without game_versions filter, and inspect if any version advertises mcVersion
	if allVers, err := queryVersions(primaryLoaders, ""); err == nil && len(allVers) > 0 {
		var matched []Version
		for _, v := range allVers {
			for _, gv := range v.GameVersions {
				if gv == mcVersion || (strings.Count(mcVersion, ".") >= 2 && gv == strings.Split(mcVersion, ".")[0]+"."+strings.Split(mcVersion, ".")[1]) {
					matched = append(matched, v)
					break
				}
			}
		}
		if len(matched) > 0 {
			return matched, nil
		}
	}

	return versions, err
}

// GetVersion returns details for a specific version ID.
func (c *Client) GetVersion(ctx context.Context, versionID string) (*Version, error) {
	var v Version
	if err := c.getJSON(ctx, "/version/"+url.PathEscape(versionID), &v); err != nil {
		return nil, fmt.Errorf("modrinth get version %q: %w", versionID, err)
	}
	return &v, nil
}

// ResolveRequiredDependencies recursively finds all required project slugs/IDs for a mod on a given mcVersion and loader.
func (c *Client) ResolveRequiredDependencies(ctx context.Context, idOrSlug, mcVersion, loader string) ([]string, error) {
	visited := make(map[string]bool)
	var resolved []string

	var traverse func(string) error
	traverse = func(curr string) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if visited[curr] {
			return nil
		}
		visited[curr] = true

		versions, err := c.GetProjectVersions(ctx, curr, mcVersion, loader)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil || len(versions) == 0 {
			// If no specific loader version for this exact MC version, try without mcVersion filter as fallback
			versions, err = c.GetProjectVersions(ctx, curr, "", loader)
			if ctx.Err() != nil {
				return ctx.Err()
			}
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
				}
				if err := traverse(depProj.Slug); err != nil {
					return err
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

// LatestVersion satisfies the modupdates.Catalog interface.
func (c *Client) LatestVersion(ctx context.Context, ns, name string) (string, []string, error) {
	slug := name
	if slug == "" {
		slug = ns
	}
	vers, err := c.GetProjectVersions(ctx, slug, "", "neoforge")
	if err != nil {
		return "", nil, err
	}
	if len(vers) == 0 {
		return "", nil, nil
	}
	return vers[0].VersionNum, nil, nil
}

// ResolveTree satisfies the modupdates.Catalog interface.
func (c *Client) ResolveTree(ctx context.Context, ns, name string) ([]string, error) {
	slug := name
	if slug == "" {
		slug = ns
	}
	return c.ResolveRequiredDependencies(ctx, slug, "", "neoforge")
}

// LatestVersionForInstance returns the latest version number and its dependency project slugs
// compatible with the instance's MCVersion and Loader.
func (c *Client) LatestVersionForInstance(ctx context.Context, ref domain.ModRef, inst domain.Instance) (string, []string, error) {
	slug := ref.Name
	if slug == "" {
		slug = ref.Namespace
	}
	loader := string(domain.NormalizeLoader(string(inst.Loader)))
	mcVer := inst.MCVersion

	vers, err := c.GetProjectVersions(ctx, slug, mcVer, loader)
	if err != nil {
		return "", nil, err
	}
	if len(vers) == 0 {
		return "", nil, nil
	}

	latest := vers[0]
	var deps []string
	for _, d := range latest.Dependencies {
		if d.DependencyType == "required" && d.ProjectID != nil {
			depProj, err := c.GetProject(ctx, *d.ProjectID)
			if err == nil && depProj != nil {
				deps = append(deps, depProj.Slug)
			}
		}
	}
	return latest.VersionNum, deps, nil
}

// ResolveTreeForInstance returns the required dependencies for a mod on this instance,
// formatted with their latest compatible versions (e.g. "slug:version") where available.
func (c *Client) ResolveTreeForInstance(ctx context.Context, ref domain.ModRef, inst domain.Instance) ([]string, error) {
	slug := ref.Name
	if slug == "" {
		slug = ref.Namespace
	}
	loader := string(domain.NormalizeLoader(string(inst.Loader)))
	mcVer := inst.MCVersion

	depSlugs, err := c.ResolveRequiredDependencies(ctx, slug, mcVer, loader)
	if err != nil {
		return nil, err
	}

	var results []string
	for _, depSlug := range depSlugs {
		depVers, err := c.GetProjectVersions(ctx, depSlug, mcVer, loader)
		if err == nil && len(depVers) > 0 {
			results = append(results, depSlug+":"+depVers[0].VersionNum)
		} else {
			results = append(results, depSlug)
		}
	}
	return results, nil
}

