package minecraft

import (
	"context"
	"strings"
	"testing"
)

func ducktopia() Slot {
	return Slot{
		Name:      "Ducktopia Farlands",
		Loader:    LoaderFabric,
		Source:    SourceModpack,
		Pack:      &Pack{Provider: ProviderCurseForge, Ref: "https://cf/ducktopia", Name: "Ducktopia Farlands"},
		MCVersion: "26.1.2",
	}
}

func TestCurseForgePackEnvOmitsVersion(t *testing.T) {
	env := ducktopia().Env()

	if _, ok := env["VERSION"]; ok {
		t.Fatal("a CurseForge pack slot must NOT set VERSION: it constrains pack-file selection and breaks the install")
	}
	if env["TYPE"] != "AUTO_CURSEFORGE" {
		t.Fatalf("TYPE = %q", env["TYPE"])
	}
	if env["CF_PAGE_URL"] == "" {
		t.Fatal("CF_PAGE_URL missing")
	}
	for _, k := range []string{"FABRIC_LOADER_VERSION", "NEOFORGE_VERSION", "MODRINTH_PROJECTS"} {
		if _, ok := env[k]; ok {
			t.Fatalf("pack slot leaked %q", k)
		}
	}
	if env["WORLD_SLOT"] != "ducktopia-farlands" {
		t.Fatalf("WORLD_SLOT = %q", env["WORLD_SLOT"])
	}
}

// The loader is domain intent even when the pack decides it: a CurseForge pack
// is not an "engine", it is a distributor. Ducktopia IS a Fabric server.
func TestPackSlotStillRecordsItsLoader(t *testing.T) {
	ann := ducktopia().Annotations()
	if ann["agrelha.ykhi.xyz/loader"] != "fabric" {
		t.Fatalf("loader annotation = %q, want fabric", ann["agrelha.ykhi.xyz/loader"])
	}
	if ann["agrelha.ykhi.xyz/source"] != "modpack" {
		t.Fatalf("source annotation = %q", ann["agrelha.ykhi.xyz/source"])
	}
	if ann["agrelha.ykhi.xyz/pack-provider"] != "curseforge" {
		t.Fatalf("provider annotation = %q", ann["agrelha.ykhi.xyz/pack-provider"])
	}
	for k := range ann {
		if !strings.HasPrefix(k, "agrelha.ykhi.xyz/") {
			t.Fatalf("annotation %q lacks the agrelha prefix", k)
		}
	}
}

// The core invariant: a pack-defined slot's loader and MC version are FACTS
// read from the pack, not settings. This is what silently destroyed a slot.
func TestPackDefinedSlotRefusesLoaderAndVersionChanges(t *testing.T) {
	m := NewSlotManager(nil, "", "")
	d := ducktopia()

	if d.CanSetLoader() || d.CanSetVersion() {
		t.Fatal("pack-defined slot must report loader/version as not settable")
	}
	if _, err := m.SetLoader(context.Background(), d, LoaderNeoForge); err == nil {
		t.Fatal("SetLoader must refuse on a pack-defined slot")
	}
	if _, err := m.SetVersion(context.Background(), d, "26.2"); err == nil {
		t.Fatal("SetVersion must refuse on a pack-defined slot")
	}
}

func TestModlistSlotAllowsChangesAndSetsProjects(t *testing.T) {
	s := Slot{Name: "atm9", Loader: LoaderNeoForge, Source: SourceModlist, MCVersion: "26.2"}
	if !s.CanSetLoader() || !s.CanSetVersion() {
		t.Fatal("a mod-list slot must allow loader/version changes")
	}
	env := s.Env()
	if env["TYPE"] != "NEOFORGE" || env["VERSION"] != "26.2" {
		t.Fatalf("env = %v", env)
	}
	if env["MODRINTH_DOWNLOAD_DEPENDENCIES"] != "required" {
		t.Fatal("mod-list slots must resolve required dependencies")
	}
	if _, ok := env["CF_PAGE_URL"]; ok {
		t.Fatal("mod-list slot leaked CF_PAGE_URL")
	}
}

func TestRoundTripThroughAnnotations(t *testing.T) {
	orig := ducktopia()
	got := SlotFromAnnotations(orig.Annotations(), orig.Env())

	if !got.PackDefined() {
		t.Fatal("round-trip lost pack definition")
	}
	if got.Loader != LoaderFabric || got.Source != SourceModpack {
		t.Fatalf("round-trip lost loader/source: %+v", got)
	}
	if got.Pack.Provider != ProviderCurseForge || got.Pack.Ref != orig.Pack.Ref {
		t.Fatalf("round-trip lost pack: %+v", got.Pack)
	}
	if got.MCVersion != "26.1.2" {
		t.Fatalf("round-trip lost mc version: %q", got.MCVersion)
	}
}

func TestSlotNameSanitises(t *testing.T) {
	for in, want := range map[string]string{
		"Ducktopia Farlands":  "ducktopia-farlands",
		"ATM9: To the Sky!":   "atm9-to-the-sky",
		"  --Weird__Name--  ": "weird-name",
		"":                    "default",
		"!!!":                 "default",
	} {
		if got := SlotName(in); got != want {
			t.Fatalf("SlotName(%q) = %q, want %q", in, got, want)
		}
	}
}

// World isolation depends on LEVEL: itzg stores the world at /data/<LEVEL>.
// subPathExpr proved unusable on this cluster, so LEVEL is the ONLY thing
// keeping one slot's world from overwriting another's.
func TestEveryEnvCarriesLevelMatchingTheSlot(t *testing.T) {
	for _, s := range []Slot{
		ducktopia(),
		{Name: "atm9", Loader: LoaderNeoForge, Source: SourceModlist, MCVersion: "26.2"},
		{Name: "Plain Vanilla", Source: SourceVanilla, MCVersion: "26.2"},
	} {
		env := s.Env()
		want := SlotName(s.Name)
		if env["LEVEL"] != want {
			t.Fatalf("slot %q: LEVEL = %q, want %q", s.Name, env["LEVEL"], want)
		}
		if env["WORLD_SLOT"] != env["LEVEL"] {
			t.Fatalf("slot %q: WORLD_SLOT %q != LEVEL %q", s.Name, env["WORLD_SLOT"], env["LEVEL"])
		}
	}
}

// Mod lists commonly include mods whose newest build for a given MC version is
// beta. Defaulting to release makes those a HARD failure that the '?' optional
// marker does not catch, because the project IS found — only its version type
// is wrong. Pack slots must not get this key (the pack pins its own versions).
func TestModListSlotsAcceptBetaVersions(t *testing.T) {
	list := Slot{Name: "fluxweave", Loader: LoaderNeoForge, Source: SourceModlist, MCVersion: "26.1.2"}.Env()
	if list["MODRINTH_PROJECTS_DEFAULT_VERSION_TYPE"] != "beta" {
		t.Fatalf("mod-list slot must accept beta builds, got %q", list["MODRINTH_PROJECTS_DEFAULT_VERSION_TYPE"])
	}
	if _, ok := ducktopia().Env()["MODRINTH_PROJECTS_DEFAULT_VERSION_TYPE"]; ok {
		t.Fatal("a pack slot must not carry Modrinth version-type settings")
	}
}
