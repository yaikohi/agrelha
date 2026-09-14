package modpack

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestParseRawModList(t *testing.T) {
	text := `# Modpack: Cobblemon Odyssey
# Comments are ignored
jei
appleskin?
fabric-api
`
	w := ParseRawModList(text)
	if w.Name != "Cobblemon Odyssey" {
		t.Fatalf("expected name Cobblemon Odyssey, got %q", w.Name)
	}
	if len(w.Slugs) != 3 {
		t.Fatalf("expected 3 slugs, got %d (%v)", len(w.Slugs), w.Slugs)
	}
	if w.Slugs[1] != "appleskin" {
		t.Fatalf("expected appleskin with stripped ?, got %q", w.Slugs[1])
	}
}

func TestParseMrpack(t *testing.T) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	indexJSON := `{
		"formatVersion": 1,
		"game": "minecraft",
		"versionId": "1.0.0",
		"name": "Super Pack",
		"dependencies": {
			"minecraft": "1.21.1",
			"fabric-loader": "0.16.0"
		},
		"files": [
			{"path": "mods/sodium-fabric-0.6.0.jar"}
		]
	}`

	f, err := zw.Create("modrinth.index.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(indexJSON)); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()

	reader := bytes.NewReader(buf.Bytes())
	w, err := ParseMrpack(reader, int64(buf.Len()))
	if err != nil {
		t.Fatalf("parse mrpack failed: %v", err)
	}
	if w.Name != "Super Pack" {
		t.Errorf("expected name Super Pack, got %q", w.Name)
	}
	if w.MCVersion != "1.21.1" {
		t.Errorf("expected MC 1.21.1, got %q", w.MCVersion)
	}
	if w.Loader != "fabric" {
		t.Errorf("expected loader fabric, got %q", w.Loader)
	}
	if len(w.Slugs) != 1 || w.Slugs[0] != "sodium" {
		t.Errorf("expected slug sodium, got %v", w.Slugs)
	}
}

func TestParsePrismZip(t *testing.T) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	mmcJSON := `{
		"formatVersion": 1,
		"components": [
			{"uid": "net.minecraft", "version": "1.20.1"},
			{"uid": "net.neoforged", "version": "47.1.106"}
		]
	}`
	f, err := zw.Create("mmc-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte(mmcJSON))

	cfg := "name=My Custom World\n"
	f2, err := zw.Create("instance.cfg")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f2.Write([]byte(cfg))

	_ = zw.Close()

	reader := bytes.NewReader(buf.Bytes())
	w, err := ParsePrismZip(reader, int64(buf.Len()))
	if err != nil {
		t.Fatalf("parse prism zip failed: %v", err)
	}
	if w.Name != "My Custom World" {
		t.Errorf("expected My Custom World, got %q", w.Name)
	}
	if w.MCVersion != "1.20.1" {
		t.Errorf("expected 1.20.1, got %q", w.MCVersion)
	}
	if w.Loader != "neoforge" {
		t.Errorf("expected loader neoforge, got %q", w.Loader)
	}
}

func makeBadZipEntry(name string) []byte {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	w, _ := zw.Create(name)
	_, _ = w.Write([]byte("data"))
	_ = zw.Close()
	data := append([]byte(nil), buf.Bytes()...)
	if len(data) > 0 {
		data[0] = 0xFF
	}
	return data
}

func makeZip(files map[string]string) []byte {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	for name, content := range files {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(content))
	}
	_ = zw.Close()
	return buf.Bytes()
}

func TestParseMrpackEdgeCases(t *testing.T) {
	// 1. Invalid zip
	if _, err := ParseMrpack(strings.NewReader("bad"), 3); err == nil {
		t.Errorf("expected error for invalid zip")
	}

	// 2. Missing modrinth.index.json
	noIndex := makeZip(map[string]string{"other.txt": "hello"})
	if _, err := ParseMrpack(bytes.NewReader(noIndex), int64(len(noIndex))); err == nil {
		t.Errorf("expected error when modrinth.index.json is missing")
	}

	// 3. Open error (unsupported compression algorithm)
	badIndex := makeBadZipEntry("modrinth.index.json")
	if _, err := ParseMrpack(bytes.NewReader(badIndex), int64(len(badIndex))); err == nil {
		t.Errorf("expected error opening modrinth.index.json")
	}

	// 4. Invalid json
	invalidJSON := makeZip(map[string]string{"modrinth.index.json": "{bad-json"})
	if _, err := ParseMrpack(bytes.NewReader(invalidJSON), int64(len(invalidJSON))); err == nil {
		t.Errorf("expected error decoding invalid json")
	}

	// 5. Dependencies: neoforge, forge, and default
	tests := []struct {
		deps       string
		wantLoader string
	}{
		{`{"minecraft": "1.21.1", "neoforge": "21.1.200"}`, "neoforge"},
		{`{"minecraft": "1.20.1", "forge": "47.1.0"}`, "neoforge"},
		{`{"minecraft": "1.21.1"}`, "neoforge"},
	}

	for _, tt := range tests {
		content := fmt.Sprintf(`{
			"formatVersion": 1,
			"game": "minecraft",
			"name": "Pack",
			"dependencies": %s,
			"files": [{"path": "mods/plain.jar"}, {"path": "mods/.jar"}, {"path": "mods/jei-1.21.jar"}]
		}`, tt.deps)
		z := makeZip(map[string]string{"modrinth.index.json": content})
		w, err := ParseMrpack(bytes.NewReader(z), int64(len(z)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if w.Loader != tt.wantLoader {
			t.Errorf("deps %s: want loader %s, got %s", tt.deps, tt.wantLoader, w.Loader)
		}
	}
}

func TestParsePrismZipEdgeCases(t *testing.T) {
	// 1. Invalid zip
	if _, err := ParsePrismZip(strings.NewReader("bad"), 3); err == nil {
		t.Errorf("expected error for invalid zip")
	}

	// 2. Missing mmc and mods
	emptyZip := makeZip(map[string]string{"dummy.txt": "dummy"})
	if _, err := ParsePrismZip(bytes.NewReader(emptyZip), int64(len(emptyZip))); err == nil {
		t.Errorf("expected error when mmc-pack.json and mods are missing")
	}

	// 3. Components: fabric-loader and minecraftforge, plus mods without hyphens
	mmcJSON := `{
		"formatVersion": 1,
		"components": [
			{"uid": "net.minecraft", "version": "1.21.1"},
			{"uid": "net.fabricmc.fabric-loader", "version": "0.16.0"}
		]
	}`
	zFabric := makeZip(map[string]string{
		"mmc-pack.json":        mmcJSON,
		"instance.cfg":         "name=FabricWorld\nother=val\n",
		"mods/plain.jar":       "dummy",
		"mods/jei-1.21.1.jar": "dummy",
		"mods/.jar":            "dummy",
	})
	wFabric, err := ParsePrismZip(bytes.NewReader(zFabric), int64(len(zFabric)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wFabric.Loader != "fabric" {
		t.Errorf("want loader fabric, got %s", wFabric.Loader)
	}
	if wFabric.Name != "FabricWorld" {
		t.Errorf("want name FabricWorld, got %s", wFabric.Name)
	}
	if len(wFabric.Slugs) != 2 {
		t.Errorf("expected 2 slugs, got %v", wFabric.Slugs)
	}

	// 4. net.minecraftforge component
	mmcForge := `{
		"formatVersion": 1,
		"components": [
			{"uid": "net.minecraftforge", "version": "47.1.0"}
		]
	}`
	zForge := makeZip(map[string]string{
		"mmc-pack.json": mmcForge,
	})
	wForge, err := ParsePrismZip(bytes.NewReader(zForge), int64(len(zForge)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wForge.Loader != "neoforge" {
		t.Errorf("want loader neoforge, got %s", wForge.Loader)
	}
}

func TestParseR2Z(t *testing.T) {
	// 1. Invalid zip
	if _, err := ParseR2Z(strings.NewReader("bad"), 3); err == nil {
		t.Errorf("expected error for invalid zip")
	}

	// 2. Missing manifest files
	noManifest := makeZip(map[string]string{"dummy.txt": "dummy"})
	if _, err := ParseR2Z(bytes.NewReader(noManifest), int64(len(noManifest))); err == nil {
		t.Errorf("expected error when neither export.r2x nor manifest.json is found")
	}

	// 3. Open error (unsupported compression algorithm)
	badR2x := makeBadZipEntry("export.r2x")
	if _, err := ParseR2Z(bytes.NewReader(badR2x), int64(len(badR2x))); err == nil {
		t.Errorf("expected error opening export.r2x")
	}

	// 4. export.r2x decode error
	invalidR2x := makeZip(map[string]string{"export.r2x": ": invalid yaml :"})
	if _, err := ParseR2Z(bytes.NewReader(invalidR2x), int64(len(invalidR2x))); err == nil {
		t.Errorf("expected error decoding invalid export.r2x")
	}

	// 5. export.r2x valid with enabled and disabled mods
	r2xContent := `profileName: ValheimPack
mods:
  - name: ModA
    enabled: true
  - name: ModB
    enabled: false
  - name: ModC
    enabled: true
`
	validR2x := makeZip(map[string]string{"export.r2x": r2xContent})
	wR2x, err := ParseR2Z(bytes.NewReader(validR2x), int64(len(validR2x)))
	if err != nil {
		t.Fatalf("unexpected error parsing valid export.r2x: %v", err)
	}
	if wR2x.Name != "ValheimPack" {
		t.Errorf("expected ValheimPack, got %s", wR2x.Name)
	}
	if len(wR2x.Slugs) != 2 || wR2x.Slugs[0] != "ModA" || wR2x.Slugs[1] != "ModC" {
		t.Errorf("expected [ModA ModC], got %v", wR2x.Slugs)
	}

	// 6. manifest.json decode error
	invalidManifest := makeZip(map[string]string{"manifest.json": "{bad-json"})
	if _, err := ParseR2Z(bytes.NewReader(invalidManifest), int64(len(invalidManifest))); err == nil {
		t.Errorf("expected error decoding invalid manifest.json")
	}

	// 7. manifest.json valid
	manifestContent := `{
		"name": "ThunderstorePack",
		"dependencies": ["denikson-BepInExPack_Valheim-5.4.2202", "ValheimPlus-3.0.0"]
	}`
	validManifest := makeZip(map[string]string{"manifest.json": manifestContent})
	wManifest, err := ParseR2Z(bytes.NewReader(validManifest), int64(len(validManifest)))
	if err != nil {
		t.Fatalf("unexpected error parsing valid manifest.json: %v", err)
	}
	if wManifest.Name != "ThunderstorePack" {
		t.Errorf("expected ThunderstorePack, got %s", wManifest.Name)
	}
	if len(wManifest.Slugs) != 2 {
		t.Errorf("expected 2 slugs, got %v", wManifest.Slugs)
	}
}
