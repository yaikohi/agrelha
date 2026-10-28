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
