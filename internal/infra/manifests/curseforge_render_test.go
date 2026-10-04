package manifests

import (
	"strings"
	"testing"

	"agrelha/internal/domain"
)

func modlistInstance() domain.Instance {
	return domain.Instance{
		GameID: domain.GameMinecraft, Number: 3, Name: "bob", Slug: "bob",
		Source: domain.SourceModlist,
		Minecraft: &domain.MinecraftConfig{
			Loader:    domain.LoaderFabric,
			MCVersion: "1.21.1",
		},
	}
}

// A world with no CurseForge mods must render exactly as before: no second
// ConfigMap key and no CURSEFORGE_FILES, or every existing world changes.
func TestRenderWithoutCurseForgeIsUnchanged(t *testing.T) {
	files, err := Render(modlistInstance(), domain.ModList{Primary: "terralith:2.6.4\n"}, "", "minecraft-modded")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	mods := string(files["mods.yaml"])
	if !strings.Contains(mods, "mods.txt: |") {
		t.Error("want the Modrinth key")
	}
	if strings.Contains(mods, "curseforge.txt") {
		t.Errorf("no CurseForge mods, so no second key:\n%s", mods)
	}
	if strings.Contains(string(files["slot.yaml"]), "CURSEFORGE_FILES") {
		t.Error("no CurseForge mods, so the server must not be told to read a CurseForge file")
	}
}

func TestRenderWithCurseForgeAddsTheSecondKeyAndEnv(t *testing.T) {
	files, err := Render(modlistInstance(),
		domain.ModList{Primary: "terralith:2.6.4\n", CurseForge: "jei:4593548\n"},
		"", "minecraft-modded")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	mods := string(files["mods.yaml"])
	for _, want := range []string{"mods.txt: |", "terralith:2.6.4", "curseforge.txt: |", "jei:4593548"} {
		if !strings.Contains(mods, want) {
			t.Errorf("mods.yaml missing %q:\n%s", want, mods)
		}
	}

	slot := string(files["slot.yaml"])
	if !strings.Contains(slot, "CURSEFORGE_FILES") {
		t.Errorf("slot.yaml must wire CURSEFORGE_FILES:\n%s", slot)
	}
	if !strings.Contains(slot, "MODRINTH_PROJECTS") {
		t.Error("both lists coexist; Modrinth must not be replaced")
	}
}
