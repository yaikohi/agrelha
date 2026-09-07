package modpack

import (
	"archive/zip"
	"bytes"
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
