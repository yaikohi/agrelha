package installed

import (
	"context"
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

func TestAnUninstalledWorldReportsNothingRatherThanGuessing(t *testing.T) {
	if _, err := New(t.TempDir()).InstalledBuild(context.Background(), domain.NewInstanceID(domain.GameMinecraft, 9)); err == nil {
		t.Error("a world with no installer manifest must not claim a build")
	}
	if _, err := New("").InstalledBuild(context.Background(), domain.NewInstanceID(domain.GameMinecraft, 1)); err == nil {
		t.Error("an unconfigured reader must refuse")
	}
}
