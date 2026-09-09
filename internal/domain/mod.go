package domain

import "time"

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
