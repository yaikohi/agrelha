package domain

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

type WorkshopCollection struct {
	ID          string
	Title       string
	ItemCount   int
	ItemIDs     []string
	TimeUpdated time.Time
}

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

	// ProviderSteamWorkshop is a collection the server resolves itself from a
	// single id, not a set of individually pinned packages.
	ProviderSteamWorkshop Provider = "steam-workshop"

	LoaderFabric   Loader = "fabric"
	LoaderNeoForge Loader = "neoforge"
	LoaderVanilla  Loader = "vanilla"
)

type Pack struct {
	Provider Provider
	Ref      string
	Name     string
}

// PackFacts is what a Pack decides about the Instance that runs it. CONTEXT.md:
// "A Pack owns its Instance's Loader and Minecraft version" - when Source is
// modpack these are facts read from the Pack, never operator settings.
type PackFacts struct {
	Loader    Loader
	MCVersion string
}

// Resolve answers what an Instance must run, given what the operator asked for.
// A Pack that declares nothing leaves the request alone: silence is not a claim
// that the Pack runs no Loader and no Minecraft version.
func (f PackFacts) Resolve(loader Loader, mcVersion string) (Loader, string) {
	if f.Loader != "" {
		loader = f.Loader
	}
	if f.MCVersion != "" {
		mcVersion = f.MCVersion
	}
	return loader, mcVersion
}

func (f PackFacts) Empty() bool { return f.Loader == "" && f.MCVersion == "" }

// CompareMCVersions orders Minecraft versions numerically, component by
// component. It spans both numbering schemes: Mojang dropped the leading "1."
// in 2026, so 26.3 is newer than 1.21.1 even though it sorts earlier as text.
func CompareMCVersions(a, b string) int {
	as, bs := mcVersionParts(a), mcVersionParts(b)
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// HighestMCVersion returns the newest of the versions given, ignoring blanks.
func HighestMCVersion(versions []string) string {
	best := ""
	for _, v := range versions {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if best == "" || CompareMCVersions(v, best) > 0 {
			best = v
		}
	}
	return best
}

func mcVersionParts(v string) []int {
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
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
