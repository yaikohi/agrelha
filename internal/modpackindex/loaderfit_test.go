package modpackindex

import "testing"

func mod(slug string, loaders ...string) Mod {
	return Mod{Slug: slug, ModrinthInfo: []ModrinthProjectRef{{Slug: slug, Loaders: loaders}}}
}

// Mirrors the real Fluxweave shape: a NeoForge pack whose mods are mostly
// multi-loader, so a naive "does fabric appear?" tally would wrongly pass it.
func fluxweaveLike() []Mod {
	var mods []Mod
	for i := 0; i < 117; i++ {
		mods = append(mods, mod("multi", "neoforge", "fabric"))
	}
	for i := 0; i < 31; i++ {
		mods = append(mods, mod("neo-only", "neoforge"))
	}
	mods = append(mods, mod("mystical-agriculture", "neoforge"))
	mods = append(mods, mod("fabric-only", "fabric"))
	return mods
}

func TestNeoForgePackRefusedOnFabric(t *testing.T) {
	mods := fluxweaveLike()

	if best := BestLoader(mods); best != "neoforge" {
		t.Fatalf("BestLoader = %q, want neoforge", best)
	}

	ok, best, target, bestFit := CheckLoader(mods, "fabric")
	if ok {
		t.Fatal("a NeoForge pack must be REFUSED on a fabric slot")
	}
	if best != "neoforge" {
		t.Fatalf("recommended loader = %q", best)
	}
	if len(target.Blocking) != 32 {
		t.Fatalf("blocking mods = %d, want 32", len(target.Blocking))
	}
	if bestFit.Supported <= target.Supported {
		t.Fatal("neoforge should support strictly more mods than fabric here")
	}

	msg := target.Explain("Fluxweave", best, bestFit)
	for _, want := range []string{"Fluxweave", "neoforge", "fabric", "mystical-agriculture"} {
		if !contains(msg, want) {
			t.Fatalf("explanation missing %q: %s", want, msg)
		}
	}
}

func TestMatchingLoaderPasses(t *testing.T) {
	if ok, _, _, _ := CheckLoader(fluxweaveLike(), "neoforge"); !ok {
		t.Fatal("the pack's own loader must pass")
	}
}

// A naive tally would see 117 fabric-capable mods and wave this through; the
// gate must key on what fabric CANNOT run, not on what it can.
func TestNaiveTallyWouldHavePassedIt(t *testing.T) {
	fab := AnalyzeLoader(fluxweaveLike(), "fabric")
	if fab.Supported < 100 {
		t.Fatalf("precondition: expected a high fabric tally, got %d", fab.Supported)
	}
	if fab.Percent() < 70 {
		t.Fatalf("precondition: expected a deceptively high %%, got %d", fab.Percent())
	}
	if ok, _, _, _ := CheckLoader(fluxweaveLike(), "fabric"); ok {
		t.Fatal("gate must refuse despite the high fabric tally")
	}
}

func TestFewStragglersTolerated(t *testing.T) {
	var mods []Mod
	for i := 0; i < 60; i++ {
		mods = append(mods, mod("multi", "neoforge", "fabric"))
	}
	mods = append(mods, mod("neo-only", "neoforge"))
	if ok, _, _, _ := CheckLoader(mods, "fabric"); !ok {
		t.Fatal("a single incompatible mod must not block a switch (it is skipped with a warning)")
	}
}

func TestModsWithoutLoaderDataExcluded(t *testing.T) {
	mods := []Mod{mod("known", "fabric"), {Slug: "unknown"}}
	f := AnalyzeLoader(mods, "fabric")
	if f.Known != 1 || f.Supported != 1 {
		t.Fatalf("mods without Modrinth data must be excluded, got %+v", f)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
