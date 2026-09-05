// Package modpackindex provides a client for the Modpack Index REST API (https://www.modpackindex.com/api/v1).
// Used by Agrelha to search, inspect, and switch Minecraft modpacks.
package modpackindex

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
	DefaultBaseURL   = "https://www.modpackindex.com/api/v1"
	DefaultUserAgent = "agrelha/0.9.0 (https://github.com/ykhi/yaya-ops)"
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
		http:      &http.Client{Timeout: 20 * time.Second},
	}
}

type ModpackSummary struct {
	ID                int               `json:"id"`
	Name              string            `json:"name"`
	Slug              string            `json:"slug"`
	Summary           string            `json:"summary"`
	URL               string            `json:"url"`
	Links             map[string]string `json:"links"`
	ThumbnailURL      string            `json:"thumbnail_url"`
	DownloadCount     int64             `json:"download_count"`
	PageURL           string            `json:"page_url"`
	LatestReleaseDate string            `json:"latest_release_date"`
}

type SearchResponse struct {
	Data  []ModpackSummary `json:"data"`
	Links struct {
		First string  `json:"first"`
		Last  string  `json:"last"`
		Prev  *string `json:"prev"`
		Next  *string `json:"next"`
	} `json:"links"`
	Meta struct {
		CurrentPage int `json:"current_page"`
		LastPage    int `json:"last_page"`
		PerPage     int `json:"per_page"`
		Total       int `json:"total"`
	} `json:"meta"`
}

type MCVersion struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type ModpackDetail struct {
	ID                int               `json:"id"`
	Name              string            `json:"name"`
	Slug              string            `json:"slug"`
	Summary           string            `json:"summary"`
	URL               string            `json:"url"`
	Links             map[string]string `json:"links"`
	ThumbnailURL      string            `json:"thumbnail_url"`
	DownloadCount     int64             `json:"download_count"`
	PageURL           string            `json:"page_url"`
	MinecraftVersions []MCVersion       `json:"minecraft_versions"`
	CurseInfo         map[string]any    `json:"curse_info"`
	ModrinthInfo      any               `json:"modrinth_info"`
}

type ModrinthProjectRef struct {
	ProjectID string   `json:"project_id"`
	Slug      string   `json:"slug"`
	Title     string   `json:"title"`
	Loaders   []string `json:"loaders"`
}

type Mod struct {
	ID           int                  `json:"id"`
	Name         string               `json:"name"`
	Slug         string               `json:"slug"`
	Summary      string               `json:"summary"`
	URL          string               `json:"url"`
	Links        map[string]string    `json:"links"`
	ModrinthInfo []ModrinthProjectRef `json:"modrinth_info"`
}

type ModsResponse struct {
	Data []Mod `json:"data"`
}

type DetailResponse struct {
	Data ModpackDetail `json:"data"`
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

// KnownMCVersionIDs maps common NeoForge-compatible Minecraft versions to Modpack Index version IDs.
var KnownMCVersionIDs = map[string]int{
	"1.21.1": 91,
	"1.21":   90,
	"1.20.6": 89,
	"1.20.4": 87,
	"1.20.2": 85,
	"1.20.1": 84,
}

// SearchModpacks queries modpacks by name and optional minecraft_version.
func (c *Client) SearchModpacks(ctx context.Context, query, mcVersion string, page int) (*SearchResponse, error) {
	if page <= 0 {
		page = 1
	}

	var endpoint string
	if query != "" {
		params := url.Values{}
		params.Set("name", query)
		params.Set("page", fmt.Sprintf("%d", page))
		endpoint = "/modpacks?" + params.Encode()
	} else if mcVersion != "" {
		if vID, ok := KnownMCVersionIDs[mcVersion]; ok {
			endpoint = fmt.Sprintf("/minecraft/version/%d/modpacks?page=%d", vID, page)
		} else {
			params := url.Values{}
			params.Set("page", fmt.Sprintf("%d", page))
			endpoint = "/modpacks?" + params.Encode()
		}
	} else {
		params := url.Values{}
		params.Set("page", fmt.Sprintf("%d", page))
		endpoint = "/modpacks?" + params.Encode()
	}

	var res SearchResponse
	if err := c.getJSON(ctx, endpoint, &res); err != nil {
		return nil, fmt.Errorf("modpackindex search: %w", err)
	}
	return &res, nil
}

// GetModpack returns full metadata and Minecraft version info for a modpack.
func (c *Client) GetModpack(ctx context.Context, id int) (*ModpackDetail, error) {
	var res DetailResponse
	endpoint := fmt.Sprintf("/modpack/%d", id)
	if err := c.getJSON(ctx, endpoint, &res); err != nil {
		return nil, fmt.Errorf("modpackindex get modpack %d: %w", id, err)
	}
	return &res.Data, nil
}

// GetModpackMods returns the list of all mods included in the specified modpack.
func (c *Client) GetModpackMods(ctx context.Context, id int) ([]Mod, error) {
	var res ModsResponse
	endpoint := fmt.Sprintf("/modpack/%d/mods", id)
	if err := c.getJSON(ctx, endpoint, &res); err != nil {
		return nil, fmt.Errorf("modpackindex get modpack mods %d: %w", id, err)
	}
	return res.Data, nil
}
