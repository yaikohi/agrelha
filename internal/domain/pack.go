package domain

import (
	"regexp"
	"strings"
)

type Source string
type Provider string
type Loader string

const (
	SourceModpack Source = "modpack"
	SourceModlist Source = "modlist"
	SourceVanilla Source = "vanilla"

	ProviderCurseForge   Provider = "curseforge"
	ProviderModrinth     Provider = "modrinth"
	ProviderThunderstore Provider = "thunderstore"

	LoaderFabric   Loader = "fabric"
	LoaderNeoForge Loader = "neoforge"
	LoaderVanilla  Loader = "vanilla"
)

type Pack struct {
	Provider Provider
	Ref      string
	Name     string
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify sanitizes a display name into a stable slug identifier.
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugUnsafe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		return "default"
	}
	return s
}

func NormalizeLoader(l string) Loader {
	if strings.EqualFold(strings.TrimSpace(string(l)), string(LoaderFabric)) {
		return LoaderFabric
	}
	return LoaderNeoForge
}

func NormalizeSource(s string) Source {
	switch Source(strings.ToLower(strings.TrimSpace(string(s)))) {
	case SourceModpack:
		return SourceModpack
	case SourceVanilla:
		return SourceVanilla
	default:
		return SourceModlist
	}
}
