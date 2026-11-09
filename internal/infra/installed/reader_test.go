package installed

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"agrelha/internal/domain"
)

func writeManifest(t *testing.T, root string, instanceNumber int, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "minecraft", domain.InstanceDirName(instanceNumber), "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Both bodies are trimmed copies of what itzg's installer actually wrote for
// instance 01 and 02 on the host.
func TestReadsTheNeoForgeManifest(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, 2, ".neoforge-manifest.json",
		`{"@type":"me.itzg.helpers.forge.ForgeManifest","minecraftVersion":"26.1.2","forgeVersion":"26.1.2.97"}`)

	got, err := New(root).InstalledBuild(context.Background(), domain.NewInstanceID(domain.GameMinecraft, 2))
	if err != nil {
		t.Fatal(err)
	}
	if got.MCVersion != "26.1.2" {
		t.Errorf("version = %q, want 26.1.2", got.MCVersion)
	}
	if got.Loader != domain.LoaderNeoForge {
		t.Errorf("loader = %q, want neoforge", got.Loader)
	}
}

func TestReadsTheFabricManifestWhereTheVersionIsNested(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, 1, ".fabric-manifest.json",
		`{"@type":"me.itzg.helpers.fabric.FabricManifest","origin":{"@type":"versions","game":"26.3","loader":"0.20.1"}}`)

	got, err := New(root).InstalledBuild(context.Background(), domain.NewInstanceID(domain.GameMinecraft, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got.MCVersion != "26.3" {
		t.Errorf("version = %q, want 26.3 from origin.game", got.MCVersion)
	}
	if got.Loader != domain.LoaderFabric {
		t.Errorf("loader = %q, want fabric", got.Loader)
	}
}

// The body is a trimmed copy of what the installer actually wrote for furuncu on
// the host. Before this manifest was read, the world had no recognised manifest
// at all and kept the version and Loader CurseForge's over-tagged index claimed.
func TestReadsTheLegacyForgeManifest(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, 1, ".forge-manifest.json",
		`{"@type":"me.itzg.helpers.forge.ForgeManifest","minecraftVersion":"1.20.1","forgeVersion":"47.4.10"}`)

	got, err := New(root).InstalledBuild(context.Background(), domain.NewInstanceID(domain.GameMinecraft, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got.MCVersion != "1.20.1" {
		t.Errorf("version = %q, want 1.20.1", got.MCVersion)
	}
	if got.Loader != domain.LoaderForge {
		t.Errorf("loader = %q, want forge", got.Loader)
	}
}

func TestAnUninstalledWorldReportsNothingRatherThanGuessing(t *testing.T) {
	if _, err := New(t.TempDir()).InstalledBuild(context.Background(), domain.NewInstanceID(domain.GameMinecraft, 9)); err == nil {
		t.Error("a world with no installer manifest must not claim a build")
	}
	if _, err := New("").InstalledBuild(context.Background(), domain.NewInstanceID(domain.GameMinecraft, 1)); err == nil {
		t.Error("an unconfigured reader must refuse")
	}
}

// The body is a trimmed copy of what itzg's AUTO_CURSEFORGE installer wrote for
// valeria on the host, files array included: the real one lists 1096 of them,
// and none of them names anything a player needs.
func TestReadsThePackTheServerInstalled(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, 2, ".curseforge-manifest.json",
		`{"@type":"me.itzg.helpers.curseforge.CurseForgeManifest",`+
			`"files":["mods/sophisticatedbackpacks-26.1.2-3.25.90.2106.jar","mods/mcw-windows-2.4.2-mc26.1neoforge.jar"],`+
			`"modpackName":"craftoria-2","modpackVersion":"0.11.0","slug":"craftoria-2",`+
			`"modId":1535822,"fileId":8732682,"fileName":"Craftoria 2 - 0.11.0",`+
			`"minecraftVersion":"26.1.2","modLoaderId":"neoforge-26.1.2.97"}`)

	got, err := New(root).InstalledPack(context.Background(), domain.NewInstanceID(domain.GameMinecraft, 2))
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != domain.ProviderCurseForge {
		t.Errorf("provider = %q, want curseforge", got.Provider)
	}
	if got.ModID != 1535822 || got.FileID != 8732682 {
		t.Errorf("pack file = %d/%d, want 1535822/8732682: those ids are the only way back to the exact release the server runs", got.ModID, got.FileID)
	}
	if got.Name != "craftoria-2" || got.Version != "0.11.0" {
		t.Errorf("pack = %q %q, want craftoria-2 0.11.0", got.Name, got.Version)
	}
}

func TestAWorldWithNoPackManifestSaysSoRatherThanGuessing(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, 3, ".neoforge-manifest.json", `{"minecraftVersion":"26.1.2","forgeVersion":"26.1.2.97"}`)

	_, err := New(root).InstalledPack(context.Background(), domain.NewInstanceID(domain.GameMinecraft, 3))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want a not-exist error so the caller can offer the pack page instead", err)
	}
}

// A manifest naming no pack file is as useless as an absent one: half a record
// must not resolve to mod 0, file 0.
func TestAPackManifestWithoutIDsIsNotAPack(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, 4, ".curseforge-manifest.json", `{"modpackName":"craftoria-2","files":[]}`)

	if _, err := New(root).InstalledPack(context.Background(), domain.NewInstanceID(domain.GameMinecraft, 4)); err == nil {
		t.Error("a manifest with no modId/fileId was accepted as a pack")
	}
}
