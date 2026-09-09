package modpackindex

import (
	"fmt"
	"sort"
	"strings"
)

// Fit reports how well a modpack's mods run on one loader.
type Fit struct {
	Loader    string
	Supported int
	Known     int
	Blocking  []string
}

func (f Fit) Percent() int {
	if f.Known == 0 {
		return 0
	}
	return f.Supported * 100 / f.Known
}

func modSupports(m Mod, loader string) bool {
	loader = strings.ToLower(loader)
	for _, mi := range m.ModrinthInfo {
		for _, l := range mi.Loaders {
			l = strings.ToLower(strings.TrimSpace(l))
			if l == loader {
				return true
			}
			// NeoForge servers load most Forge-era mods; Modrinth still tags many
			// of them "forge" only.
			if loader == "neoforge" && l == "forge" {
				return true
			}
		}
	}
	return false
}

// AnalyzeLoader measures a pack against one loader. Mods with no Modrinth
// loader data are excluded from the denominator rather than assumed compatible.
func AnalyzeLoader(mods []Mod, loader string) Fit {
	f := Fit{Loader: strings.ToLower(loader)}
	for _, m := range mods {
		if len(m.ModrinthInfo) == 0 {
			continue
		}
		f.Known++
		if modSupports(m, loader) {
			f.Supported++
		} else if m.Slug != "" {
			f.Blocking = append(f.Blocking, m.Slug)
		}
	}
	sort.Strings(f.Blocking)
	return f
}

// BestLoader returns the loader the pack is actually built for.
func BestLoader(mods []Mod) string {
	best, bestN := "neoforge", -1
	for _, l := range []string{"neoforge", "fabric"} {
		if n := AnalyzeLoader(mods, l).Supported; n > bestN {
			best, bestN = l, n
		}
	}
	return best
}

// CheckLoader reports whether a pack can reasonably run on the target loader.
// A raw per-loader tally is misleading because most mods are multi-loader — a
// NeoForge-only pack can still show a high "fabric" count. What matters is how
// many mods the target loader CANNOT run compared with the best alternative.
func CheckLoader(mods []Mod, target string) (ok bool, best string, targetFit, bestFit Fit) {
	target = strings.ToLower(strings.TrimSpace(target))
	best = BestLoader(mods)
	targetFit = AnalyzeLoader(mods, target)
	bestFit = AnalyzeLoader(mods, best)

	if best == target || targetFit.Known == 0 {
		return true, best, targetFit, bestFit
	}

	// Tolerate a few stragglers (they are skipped with a warning), but refuse
	// when a materially better loader exists.
	margin := bestFit.Supported - targetFit.Supported
	limit := targetFit.Known / 20
	if limit < 5 {
		limit = 5
	}
	return margin < limit, best, targetFit, bestFit
}

func (f Fit) Explain(packName, best string, bestFit Fit) string {
	sample := f.Blocking
	if len(sample) > 5 {
		sample = sample[:5]
	}
	return fmt.Sprintf(
		"%q is a %s pack: %d of its %d mods cannot run on %s (e.g. %s). On %s, %d/%d work. Switch the slot to %s, or pick a %s pack.",
		packName, best, len(f.Blocking), f.Known, f.Loader, strings.Join(sample, ", "),
		best, bestFit.Supported, bestFit.Known, best, f.Loader,
	)
}
