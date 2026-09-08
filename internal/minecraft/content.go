package minecraft

import (
	"regexp"
	"strings"
)

// Content vocabulary shared by every Minecraft server model. See CONTEXT.md:
// Loader is the engine, Source is how content is defined, and Provider is who
// distributes a Pack. They are deliberately orthogonal — conflating them into
// itzg's single TYPE is what once let a CurseForge pack be treated as a "type"
// of server and silently destroyed.
type Source string
type Provider string
type Loader string

const (
	SourceModpack Source = "modpack"
	SourceModlist Source = "modlist"
	SourceVanilla Source = "vanilla"

	ProviderCurseForge Provider = "curseforge"
	ProviderModrinth   Provider = "modrinth"

	LoaderFabric   Loader = "fabric"
	LoaderNeoForge Loader = "neoforge"

	annPrefix = "agrelha.ykhi.xyz/"
)

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// Pack is a published, versioned mod collection. When a server has one, the Pack
// decides its Loader and Minecraft version.
type Pack struct {
	Provider Provider
	Ref      string
	Name     string
}

// SlotName sanitises a display name into a stable identifier.
func SlotName(s string) string {
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
	if strings.EqualFold(strings.TrimSpace(l), string(LoaderFabric)) {
		return LoaderFabric
	}
	return LoaderNeoForge
}

func NormalizeSource(s string) Source {
	switch Source(strings.ToLower(strings.TrimSpace(s))) {
	case SourceModpack:
		return SourceModpack
	case SourceVanilla:
		return SourceVanilla
	default:
		return SourceModlist
	}
}
