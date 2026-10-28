package domain

import "testing"

func TestHighestMCVersionSpansBothNumberingSchemes(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"new scheme beats old", []string{"1.21.1", "26.3", "1.20.1"}, "26.3"},
		{"within the old scheme", []string{"1.20.1", "1.21.1", "1.9"}, "1.21.1"},
		{"within the new scheme", []string{"26.1.2", "26.3", "26.2"}, "26.3"},
		{"blanks ignored", []string{"", "  ", "1.21.1"}, "1.21.1"},
		{"loader names are not versions", []string{"26.3", "Fabric"}, "26.3"},
		{"nothing but a loader name", []string{"Fabric"}, ""},
		{"the stale index case", []string{"1.21.11", "26.1", "26.1.2"}, "26.1.2"},
		{"nothing declared", nil, ""},
		{"single entry", []string{"26.3"}, "26.3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HighestMCVersion(c.in); got != c.want {
				t.Errorf("HighestMCVersion(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestCompareMCVersionsOrdersNumericallyNotAsText(t *testing.T) {
	if CompareMCVersions("26.3", "1.21.1") <= 0 {
		t.Error("26.3 is newer than 1.21.1; sorting these as text gets it backwards")
	}
	if CompareMCVersions("1.21.1", "1.9") <= 0 {
		t.Error("1.21.1 is newer than 1.9; sorting these as text gets it backwards")
	}
	if CompareMCVersions("1.21.1", "1.21.1") != 0 {
		t.Error("a version must equal itself")
	}
	if CompareMCVersions("1.21.1-rc1", "1.21.1") != 0 {
		t.Error("a suffix must not change ordering")
	}
}

func TestPackFactsResolve(t *testing.T) {
	cases := []struct {
		name        string
		facts       PackFacts
		loader      Loader
		version     string
		wantLoader  Loader
		wantVersion string
	}{
		{
			name:        "the Pack overrules what the operator asked for",
			facts:       PackFacts{Loader: LoaderNeoForge, MCVersion: "26.3"},
			loader:      LoaderFabric,
			version:     "1.21.1",
			wantLoader:  LoaderNeoForge,
			wantVersion: "26.3",
		},
		{
			name:        "a Pack that declares only a Loader leaves the version alone",
			facts:       PackFacts{Loader: LoaderFabric},
			loader:      LoaderNeoForge,
			version:     "1.21.1",
			wantLoader:  LoaderFabric,
			wantVersion: "1.21.1",
		},
		{
			name:        "a Pack that declares only a version leaves the Loader alone",
			facts:       PackFacts{MCVersion: "26.3"},
			loader:      LoaderFabric,
			version:     "1.21.1",
			wantLoader:  LoaderFabric,
			wantVersion: "26.3",
		},
		{
			name:        "a Pack that declares nothing changes nothing",
			facts:       PackFacts{},
			loader:      LoaderFabric,
			version:     "1.21.1",
			wantLoader:  LoaderFabric,
			wantVersion: "1.21.1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotLoader, gotVersion := c.facts.Resolve(c.loader, c.version)
			if gotLoader != c.wantLoader {
				t.Errorf("loader = %q, want %q", gotLoader, c.wantLoader)
			}
			if gotVersion != c.wantVersion {
				t.Errorf("version = %q, want %q", gotVersion, c.wantVersion)
			}
		})
	}
}

func TestPackFactsOverLayersAnAuthorityOnAFallback(t *testing.T) {
	index := PackFacts{Loader: LoaderFabric, MCVersion: "26.1.2"}
	curseforge := PackFacts{MCVersion: "26.3"}

	got := curseforge.Over(index)
	if got.MCVersion != "26.3" {
		t.Errorf("version = %q; the authority must win where it speaks", got.MCVersion)
	}
	if got.Loader != LoaderFabric {
		t.Errorf("loader = %q; the fallback must fill in what the authority omits", got.Loader)
	}

	if silent := (PackFacts{}).Over(index); silent != index {
		t.Errorf("a silent authority must change nothing, got %+v", silent)
	}
}

func TestParseLoaderDoesNotGuess(t *testing.T) {
	if got := ParseLoader("Fabric"); got != LoaderFabric {
		t.Errorf("ParseLoader(Fabric) = %q", got)
	}
	if got := ParseLoader("NeoForge"); got != LoaderNeoForge {
		t.Errorf("ParseLoader(NeoForge) = %q", got)
	}
	if got := ParseLoader("Forge"); got != "" {
		t.Errorf("ParseLoader(Forge) = %q; agrelha cannot create a Forge Instance, so it must not claim one", got)
	}
	if got := ParseLoader(""); got != "" {
		t.Errorf("ParseLoader(\"\") = %q; silence is not NeoForge", got)
	}
	if got := NormalizeLoader(""); got != LoaderNeoForge {
		t.Errorf("NormalizeLoader still guesses for the operator path, got %q", got)
	}
}

func TestPackFactsEmpty(t *testing.T) {
	if !(PackFacts{}).Empty() {
		t.Error("a Pack that declares nothing is empty")
	}
	if (PackFacts{MCVersion: "26.3"}).Empty() {
		t.Error("a declared version is not empty")
	}
	if (PackFacts{Loader: LoaderFabric}).Empty() {
		t.Error("a declared Loader is not empty")
	}
}
