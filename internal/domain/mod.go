package domain

import (
	"strconv"
	"strings"
	"time"
)

// ModProject represents a mod project from an upstream repository.
type ModProject struct {
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

// ModVersionFile represents an artifact/jar file belonging to a mod version.
type ModVersionFile struct {
	Hashes   map[string]string `json:"hashes"` // "sha1", "sha512"
	URL      string            `json:"url"`
	FileName string            `json:"filename"`
	Primary  bool              `json:"primary"`
	Size     int64             `json:"size"`
}

// VersionDependency represents a relationship between mod versions or projects.
type VersionDependency struct {
	VersionID      *string `json:"version_id"`
	ProjectID      *string `json:"project_id"`
	FileName       *string `json:"file_name"`
	DependencyType string  `json:"dependency_type"` // "required", "optional", "incompatible", "embedded"
}

// ModVersion represents a specific release of a mod project.
type ModVersion struct {
	ID           string              `json:"id"`
	ProjectID    string              `json:"project_id"`
	AuthorID     string              `json:"author_id"`
	Name         string              `json:"name"`
	VersionNum   string              `json:"version_number"`
	GameVersions []string            `json:"game_versions"`
	Loaders      []string            `json:"loaders"`
	Files        []ModVersionFile    `json:"files"`
	Dependencies []VersionDependency `json:"dependencies"`
	DatePub      time.Time           `json:"date_published"`
}

// ModSearchResult represents a summary search result for a mod package.
type ModSearchResult struct {
	Owner        string    `json:"owner"`
	Name         string    `json:"name"`
	FullURL      string    `json:"full_url"`
	Description  string    `json:"description"`
	Icon         string    `json:"icon"`
	Version      string    `json:"version"`
	Downloads    int64     `json:"downloads"`
	IsDeprecated bool      `json:"is_deprecated"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// FullName returns owner/name identifier.
func (r ModSearchResult) FullName() string { return r.Owner + "/" + r.Name }

// ModRef identifies one installed mod independently of how its entry is
// written. Valheim entries are Thunderstore packages, either the bare full name
// "Namespace-Name" or the pinned "Namespace/Name/Version". Minecraft entries are
// Modrinth slugs, which legitimately contain hyphens ("cloth-config"), so they
// are never split on one.
type ModRef struct {
	Namespace string
	Name      string
	Version   string
}

func ParseModRef(entry string, game GameID) (ModRef, bool) {
	entry = strings.TrimSuffix(strings.TrimSpace(entry), "?")
	if entry == "" || strings.HasPrefix(entry, "#") {
		return ModRef{}, false
	}

	if parts := strings.Split(entry, "/"); len(parts) >= 3 {
		return ModRef{
			Namespace: strings.Join(parts[:len(parts)-2], "/"),
			Name:      parts[len(parts)-2],
			Version:   parts[len(parts)-1],
		}, true
	}

	if game == GameValheim {
		if ns, rest, ok := strings.Cut(entry, "-"); ok && ns != "" && rest != "" {
			// Thunderstore's own copy button gives Namespace-Name-Version.
			// Package names use underscores, never hyphens, so a trailing
			// semver segment is the version rather than part of the name.
			if name, version, ok := strings.Cut(rest, "-"); ok && isSemver(version) {
				return ModRef{Namespace: ns, Name: name, Version: version}, true
			}
			return ModRef{Namespace: ns, Name: rest}, true
		}
	}
	return ModRef{Name: entry}, true
}

// Key is the identity two entries share when they are the same mod at different
// versions. Removing and de-duplicating compare on this, never on the raw line.
func (r ModRef) Key() string {
	if r.Namespace == "" {
		return strings.ToLower(r.Name)
	}
	return strings.ToLower(r.Namespace + "-" + r.Name)
}

// FullName is the Thunderstore "Namespace-Name" form, which is what r2modman
// and agrelha's own search results use.
func (r ModRef) FullName() string {
	if r.Namespace == "" {
		return r.Name
	}
	return r.Namespace + "-" + r.Name
}

// Entry renders the line to store in mods.txt: pinned when the version is
// known, so an export reproduces exactly what the server runs.
func (r ModRef) Entry() string {
	if r.Namespace != "" && r.Version != "" {
		return r.Namespace + "/" + r.Name + "/" + r.Version
	}
	if r.Namespace != "" {
		return r.Namespace + "-" + r.Name
	}
	return r.Name
}

// isSemver reports whether s looks like a Thunderstore version: major.minor.patch.
func isSemver(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// ModUpdate is one installed mod whose upstream catalogue carries a newer
// version than the entry pinned in the Instance's mod list.
type ModUpdate struct {
	Ref     ModRef
	Current string
	Latest  string
}

// CatalogKey is how the upstream package index keys this mod: "Namespace/Name",
// matching ModSearchResult.FullName. It is deliberately not ModRef.FullName,
// which is Thunderstore's hyphenated display form.
func (r ModRef) CatalogKey() string {
	if r.Namespace == "" {
		return r.Name
	}
	return r.Namespace + "/" + r.Name
}

// VersionNewer reports whether version a is strictly newer than b. Versions are
// compared segment by segment as numbers, so 1.10.0 sorts above 1.9.0.
func VersionNewer(a, b string) bool {
	as := strings.Split(strings.TrimSpace(a), ".")
	bs := strings.Split(strings.TrimSpace(b), ".")
	n := max(len(as), len(bs))
	for i := range n {
		var av, bv int
		if i < len(as) {
			av, _ = strconv.Atoi(strings.TrimSpace(as[i]))
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(strings.TrimSpace(bs[i]))
		}
		if av != bv {
			return av > bv
		}
	}
	return false
}

// ModRestorePoint is the way back from one Mod update: the list as it was
// before, and the list the update wrote. Undo compares Applied against what the
// Instance reports now, and refuses when they differ — something else has
// changed the Mod list since, and reverting would discard it.
type ModRestorePoint struct {
	At       time.Time
	Previous []string
	Applied  []string
}

// Matches reports whether entries are exactly what this update wrote, in any order.
func (p ModRestorePoint) Matches(entries []string) bool {
	if len(entries) != len(p.Applied) {
		return false
	}
	have := make(map[string]int, len(entries))
	for _, e := range entries {
		have[strings.TrimSpace(e)]++
	}
	for _, e := range p.Applied {
		e = strings.TrimSpace(e)
		if have[e] == 0 {
			return false
		}
		have[e]--
	}
	return true
}
