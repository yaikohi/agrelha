package domain

import "testing"

func TestParseMCModRefTakesProviderFromTheFileNotTheLine(t *testing.T) {
	// The two syntaxes are indistinguishable: "jei:4593548" is a CurseForge file
	// id and "terralith:2.6.4" a Modrinth version, but nothing in the text says
	// so. Guessing would send a file id to Modrinth, which only fails at boot.
	cf, ok := ParseMCModRef("jei:4593548", ProviderCurseForge)
	if !ok || cf.Provider != ProviderCurseForge || cf.Slug != "jei" || cf.Pin != "4593548" {
		t.Fatalf("curseforge parse wrong: %+v ok=%v", cf, ok)
	}
	mr, ok := ParseMCModRef("jei:4593548", ProviderModrinth)
	if !ok || mr.Provider != ProviderModrinth {
		t.Fatalf("the same text must parse as Modrinth when it came from the Modrinth file: %+v", mr)
	}
	if cf.Key() == mr.Key() {
		t.Error("the same slug from different Providers is not the same mod")
	}
}

func TestParseMCModRefSkipsBlanksAndComments(t *testing.T) {
	for _, line := range []string{"", "   ", "# a note", "\t# indented"} {
		if _, ok := ParseMCModRef(line, ProviderModrinth); ok {
			t.Errorf("line %q should not parse as a mod", line)
		}
	}
}

func TestMCModRefRoundTrips(t *testing.T) {
	body := "terralith:2.6.4\n# comment\n\ntectonic:3.0.28-fabric-26.2\nnopin\n"
	refs := SplitModLines(body, ProviderModrinth)
	if len(refs) != 3 {
		t.Fatalf("want 3 refs, got %d: %+v", len(refs), refs)
	}
	if refs[2].Pin != "" || refs[2].Entry() != "nopin" {
		t.Errorf("an unpinned entry must survive untouched, got %q", refs[2].Entry())
	}
	want := "terralith:2.6.4\ntectonic:3.0.28-fabric-26.2\nnopin\n"
	if got := JoinModLines(refs); got != want {
		t.Errorf("round trip = %q, want %q", got, want)
	}
}

func TestJoinModLinesEmptyStaysEmpty(t *testing.T) {
	// An empty body must stay empty rather than becoming "\n": the renderer uses
	// emptiness to decide whether the server is told to read the file at all.
	if got := JoinModLines(nil); got != "" {
		t.Errorf("want empty, got %q", got)
	}
}

// A world with no CurseForge entries must not be told to read a CurseForge file,
// or the server looks for one that was never written.
func TestEnvOnlyPointsAtCurseForgeWhenThereAreCurseForgeMods(t *testing.T) {
	inst := Instance{
		GameID: GameMinecraft, Number: 3, Name: "bob", Slug: "bob",
		Source: SourceModlist, Loader: LoaderFabric, MCVersion: "1.21.1",
	}

	without := inst.EnvWith(ModList{Primary: "terralith:2.6.4\n"})
	if _, ok := without["CURSEFORGE_FILES"]; ok {
		t.Error("no CurseForge mods, so no CURSEFORGE_FILES")
	}
	if without["MODRINTH_PROJECTS"] != "@/config-mods/mods.txt" {
		t.Error("Modrinth must be unaffected")
	}

	with := inst.EnvWith(ModList{Primary: "terralith:2.6.4\n", CurseForge: "jei:4593548\n"})
	if with["CURSEFORGE_FILES"] != "@/config-mods/curseforge.txt" {
		t.Errorf("want the CurseForge list wired, got %q", with["CURSEFORGE_FILES"])
	}
	if with["MODRINTH_PROJECTS"] != "@/config-mods/mods.txt" {
		t.Error("both lists coexist; Modrinth must not be replaced")
	}
}

// Env() is the no-mods-known case and must stay conservative.
func TestEnvWithoutModsKnownNeverWiresCurseForge(t *testing.T) {
	inst := Instance{
		GameID: GameMinecraft, Number: 3, Slug: "bob",
		Source: SourceModlist, Loader: LoaderFabric, MCVersion: "1.21.1",
	}
	if _, ok := inst.Env()["CURSEFORGE_FILES"]; ok {
		t.Error("Env() knows of no mods, so it must not point at a CurseForge file")
	}
}

// A Pack-defined world is driven entirely by the Pack; a stray Mod list must not
// start injecting env into it.
func TestPackDefinedIgnoresModList(t *testing.T) {
	inst := Instance{
		GameID: GameMinecraft, Number: 1, Slug: "fluxweave", Source: SourceModpack,
		Pack: &Pack{Provider: ProviderCurseForge, Ref: "https://curseforge.com/x", Name: "Fluxweave"},
	}
	env := inst.EnvWith(ModList{CurseForge: "jei:4593548\n"})
	if env["TYPE"] != "AUTO_CURSEFORGE" {
		t.Fatalf("want AUTO_CURSEFORGE, got %q", env["TYPE"])
	}
	if _, ok := env["CURSEFORGE_FILES"]; ok {
		t.Error("a Pack owns its mods; CURSEFORGE_FILES must not appear")
	}
}
