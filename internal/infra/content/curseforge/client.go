// Package curseforge is agrelha's client for the CurseForge v1 API, used to
// search for Minecraft mods, pin a file id, and resolve dependencies.
//
// Unlike Modrinth, CurseForge has no anonymous access: every call carries an API
// key. A nil client therefore means "CurseForge is not configured", which
// callers render as an absent catalogue rather than an error - Modrinth keeps
// working on its own.
package curseforge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

// gameIDMinecraft is CurseForge's own identifier for Minecraft.
const gameIDMinecraft = 432

// classIDMods narrows searches to mods, excluding modpacks, resource packs and
// worlds, which share the same search endpoint.
const classIDMods = 6

// modLoaderType values, from CurseForge's enum (1=Forge, 4=Fabric, 6=NeoForge).
// agrelha models only the two Loaders it can actually create, so Forge has no
// entry: an Instance can never ask for it.
var modLoaderType = map[domain.Loader]int{
	domain.LoaderFabric:   4,
	domain.LoaderNeoForge: 6,
}

// ErrRestricted means the author forbids third-party distribution. The mod
// exists and can be described, but no download URL exists for anyone but
// CurseForge's own client, and no API key changes that.
var ErrRestricted = errors.New("this mod's author does not allow third-party downloads, so it cannot be installed automatically")

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func New(baseURL, apiKey string) *Client {
	if strings.TrimSpace(apiKey) == "" {
		return nil
	}
	if baseURL == "" {
		baseURL = "https://api.curseforge.com/v1"
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

// Ready reports whether CurseForge can be asked anything at all.
func (c *Client) Ready() bool { return c != nil && c.apiKey != "" }

func (c *Client) getJSON(ctx context.Context, path string, v any) error {
	if !c.Ready() {
		return ports.ErrNotImplemented
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%s: %w", path, ports.ErrPackageNotFound)
	case resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("curseforge rejected the API key (%s)", resp.Status)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("%s -> %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

type modDTO struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Summary string `json:"summary"`
	Logo    struct {
		ThumbnailURL string `json:"thumbnailUrl"`
	} `json:"logo"`
	Links struct {
		WebsiteURL string `json:"websiteUrl"`
	} `json:"links"`
	DownloadCount        float64 `json:"downloadCount"`
	AllowModDistribution *bool   `json:"allowModDistribution"`
}

// Restricted reports whether the author forbids third-party distribution.
// CurseForge omits the field for most mods, and absent means allowed.
func (m modDTO) Restricted() bool {
	return m.AllowModDistribution != nil && !*m.AllowModDistribution
}

type fileDTO struct {
	ID           int      `json:"id"`
	DisplayName  string   `json:"displayName"`
	FileName     string   `json:"fileName"`
	DownloadURL  string   `json:"downloadUrl"`
	FileDate     string   `json:"fileDate"`
	FileLength   int64    `json:"fileLength"`
	GameVersions []string `json:"gameVersions"`
	Hashes       []struct {
		Value string `json:"value"`
		Algo  int    `json:"algo"`
	} `json:"hashes"`
	Dependencies []struct {
		ModID        int `json:"modId"`
		RelationType int `json:"relationType"`
	} `json:"dependencies"`
}

// relationRequired is CurseForge's enum value for a required dependency. The
// others (optional, embedded, incompatible, tool) must not be installed
// automatically.
const relationRequired = 3

// Search returns mods matching query that can actually run on this world. The
// game version and loader filters are applied by CurseForge rather than locally:
// an unfiltered search is mostly mods the Instance cannot load.
func (c *Client) Search(ctx context.Context, query, mcVersion string, loader domain.Loader, limit int) ([]domain.CFMod, error) {
	if !c.Ready() {
		return nil, nil
	}
	if limit <= 0 {
		limit = 25
	}
	q := url.Values{}
	q.Set("gameId", strconv.Itoa(gameIDMinecraft))
	q.Set("classId", strconv.Itoa(classIDMods))
	q.Set("pageSize", strconv.Itoa(limit))
	q.Set("sortField", "2") // popularity
	q.Set("sortOrder", "desc")
	if strings.TrimSpace(query) != "" {
		q.Set("searchFilter", query)
	}
	if strings.TrimSpace(mcVersion) != "" {
		q.Set("gameVersion", mcVersion)
	}
	if t, ok := modLoaderType[domain.NormalizeLoader(string(loader))]; ok {
		q.Set("modLoaderType", strconv.Itoa(t))
	}

	var body struct {
		Data []modDTO `json:"data"`
	}
	if err := c.getJSON(ctx, "/mods/search?"+q.Encode(), &body); err != nil {
		return nil, err
	}

	out := make([]domain.CFMod, 0, len(body.Data))
	for _, m := range body.Data {
		out = append(out, toDomain(m))
	}
	return out, nil
}

// GetMod fetches one mod by slug or numeric id.
func (c *Client) GetMod(ctx context.Context, slugOrID string) (*domain.CFMod, error) {
	if !c.Ready() {
		return nil, nil
	}
	if id, err := strconv.Atoi(strings.TrimSpace(slugOrID)); err == nil {
		var body struct {
			Data modDTO `json:"data"`
		}
		if err := c.getJSON(ctx, fmt.Sprintf("/mods/%d", id), &body); err != nil {
			return nil, err
		}
		m := toDomain(body.Data)
		return &m, nil
	}

	// No by-slug endpoint exists; search is the documented way to resolve one.
	q := url.Values{}
	q.Set("gameId", strconv.Itoa(gameIDMinecraft))
	q.Set("classId", strconv.Itoa(classIDMods))
	q.Set("slug", strings.TrimSpace(slugOrID))
	var body struct {
		Data []modDTO `json:"data"`
	}
	if err := c.getJSON(ctx, "/mods/search?"+q.Encode(), &body); err != nil {
		return nil, err
	}
	if len(body.Data) == 0 {
		return nil, fmt.Errorf("%s: %w", slugOrID, ports.ErrPackageNotFound)
	}
	m := toDomain(body.Data[0])
	return &m, nil
}

func toDomain(m modDTO) domain.CFMod {
	return domain.CFMod{
		ID:         m.ID,
		Slug:       m.Slug,
		Name:       m.Name,
		Summary:    m.Summary,
		IconURL:    m.Logo.ThumbnailURL,
		PageURL:    m.Links.WebsiteURL,
		Downloads:  int64(m.DownloadCount),
		Restricted: m.Restricted(),
	}
}

// latestFile returns the newest file for this Minecraft version and Loader.
func (c *Client) latestFile(ctx context.Context, modID int, mcVersion string, loader domain.Loader) (*fileDTO, error) {
	q := url.Values{}
	q.Set("pageSize", "50")
	if strings.TrimSpace(mcVersion) != "" {
		q.Set("gameVersion", mcVersion)
	}
	if t, ok := modLoaderType[domain.NormalizeLoader(string(loader))]; ok {
		q.Set("modLoaderType", strconv.Itoa(t))
	}
	var body struct {
		Data []fileDTO `json:"data"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("/mods/%d/files?%s", modID, q.Encode()), &body); err != nil {
		return nil, err
	}
	if len(body.Data) == 0 {
		return nil, fmt.Errorf("no file for Minecraft %s / %s: %w", mcVersion, loader, ports.ErrPackageNotFound)
	}
	files := body.Data
	sort.SliceStable(files, func(i, j int) bool { return files[i].FileDate > files[j].FileDate })
	return &files[0], nil
}

// Resolve turns a slug into its own pinned entry plus a pinned entry per
// required dependency, depth-first and de-duplicated.
//
// The server cannot do this: CurseForge dependency records name a mod id and
// never a file id, so choosing a file needs the Instance's Minecraft version and
// Loader. See ADR 0004. Every returned entry is "<slug>:<fileId>".
func (c *Client) Resolve(ctx context.Context, slugOrID, mcVersion, loader string) ([]string, error) {
	if !c.Ready() {
		return nil, ports.ErrNotImplemented
	}
	ld := domain.NormalizeLoader(loader)

	root, err := c.GetMod(ctx, slugOrID)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("%s: %w", slugOrID, ports.ErrPackageNotFound)
	}
	if root.Restricted {
		return nil, fmt.Errorf("%s: %w", root.Name, ErrRestricted)
	}

	var out []string
	seen := map[int]bool{}
	queue := []int{root.ID}
	slugs := map[int]string{root.ID: root.Slug}

	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true

		file, err := c.latestFile(ctx, id, mcVersion, ld)
		if err != nil {
			// A dependency with no build for this world is fatal here rather than
			// at boot: the operator can still choose a different mod.
			return nil, fmt.Errorf("%s: %w", slugs[id], err)
		}

		slug := slugs[id]
		if slug == "" {
			m, err := c.GetMod(ctx, strconv.Itoa(id))
			if err != nil {
				return nil, err
			}
			if m.Restricted {
				return nil, fmt.Errorf("%s (required by %s): %w", m.Name, root.Name, ErrRestricted)
			}
			slug = m.Slug
			slugs[id] = slug
		}
		out = append(out, fmt.Sprintf("%s:%d", slug, file.ID))

		for _, d := range file.Dependencies {
			if d.RelationType == relationRequired && !seen[d.ModID] {
				queue = append(queue, d.ModID)
			}
		}
	}
	return out, nil
}

// DownloadURL returns the CDN URL for a pinned file, so an export can embed the
// mod rather than merely naming it. Empty when the author forbids distribution.
func (c *Client) DownloadURL(ctx context.Context, modID, fileID int) (string, error) {
	if !c.Ready() {
		return "", ports.ErrNotImplemented
	}
	var body struct {
		Data fileDTO `json:"data"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("/mods/%d/files/%d", modID, fileID), &body); err != nil {
		return "", err
	}
	return body.Data.DownloadURL, nil
}

// hashAlgoSHA1 is CurseForge's enum value for SHA1.
const hashAlgoSHA1 = 1

func (f fileDTO) sha1() string {
	for _, h := range f.Hashes {
		if h.Algo == hashAlgoSHA1 {
			return h.Value
		}
	}
	return ""
}

// ExportFile is a pinned CurseForge file with everything an export needs to
// embed it. URL is empty for a Restricted mod, which no modpack may carry.
type ExportFile struct {
	Slug     string
	FileName string
	URL      string
	SHA1     string
	Size     int64
}

// ExportFiles resolves "<slug>:<fileId>" entries into embeddable files. An entry
// it cannot resolve is returned with an empty URL rather than dropped, so the
// export can name it in its report instead of silently omitting a mod the server
// runs.
func (c *Client) ExportFiles(ctx context.Context, entries []string) ([]ExportFile, error) {
	if !c.Ready() {
		return nil, nil
	}
	out := make([]ExportFile, 0, len(entries))
	for _, e := range entries {
		ref, ok := domain.ParseMCModRef(e, domain.ProviderCurseForge)
		if !ok {
			continue
		}
		file := ExportFile{Slug: ref.Slug}

		mod, err := c.GetMod(ctx, ref.Slug)
		if err != nil || mod == nil || mod.Restricted || ref.Pin == "" {
			out = append(out, file)
			continue
		}
		fileID, err := strconv.Atoi(ref.Pin)
		if err != nil {
			out = append(out, file)
			continue
		}

		var body struct {
			Data fileDTO `json:"data"`
		}
		if err := c.getJSON(ctx, fmt.Sprintf("/mods/%d/files/%d", mod.ID, fileID), &body); err != nil {
			out = append(out, file)
			continue
		}
		file.FileName = body.Data.FileName
		file.URL = body.Data.DownloadURL
		file.SHA1 = body.Data.sha1()
		file.Size = body.Data.FileLength
		out = append(out, file)
	}
	return out, nil
}
