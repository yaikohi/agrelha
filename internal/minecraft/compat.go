package minecraft

import (
	"context"
	"strings"

	"agrelha/internal/modrinth"
)

type CartCompatibility struct {
	TotalMods   int      `json:"total_mods"`
	NeoForgeFit int      `json:"neoforge_fit"`
	FabricFit   int      `json:"fabric_fit"`
	BestLoader  string   `json:"best_loader"`
	NeoBlocking []string `json:"neo_blocking"`
	FabBlocking []string `json:"fab_blocking"`
}

func CheckCartCompatibility(ctx context.Context, mr *modrinth.Client, slugs []string, mcVersion string) CartCompatibility {
	if len(slugs) == 0 || mr == nil {
		return CartCompatibility{BestLoader: "neoforge"}
	}

	cleanSlugs := make([]string, 0, len(slugs))
	for _, s := range slugs {
		s = strings.TrimSpace(strings.TrimSuffix(s, "?"))
		if s != "" && !strings.HasPrefix(s, "#") {
			cleanSlugs = append(cleanSlugs, s)
		}
	}

	projects, err := mr.GetProjects(ctx, cleanSlugs)
	if err != nil || len(projects) == 0 {
		return CartCompatibility{
			TotalMods:   len(cleanSlugs),
			NeoForgeFit: len(cleanSlugs),
			FabricFit:   len(cleanSlugs),
			BestLoader:  "neoforge",
		}
	}

	compat := CartCompatibility{
		TotalMods: len(projects),
	}

	for _, p := range projects {
		hasFabric := false
		hasNeoForge := false

		for _, l := range p.Loaders {
			l = strings.ToLower(strings.TrimSpace(l))
			if l == "fabric" {
				hasFabric = true
			}
			if l == "neoforge" || l == "forge" {
				hasNeoForge = true
			}
		}

		if hasFabric {
			compat.FabricFit++
		} else {
			compat.FabBlocking = append(compat.FabBlocking, p.Slug)
		}

		if hasNeoForge {
			compat.NeoForgeFit++
		} else {
			compat.NeoBlocking = append(compat.NeoBlocking, p.Slug)
		}
	}

	if compat.NeoForgeFit >= compat.FabricFit {
		compat.BestLoader = "neoforge"
	} else {
		compat.BestLoader = "fabric"
	}

	return compat
}
