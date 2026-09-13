package modpack

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestBuildKeepsFullNameEntries(t *testing.T) {
	data, err := Build("boppo", []string{
		"denikson/BepInExPack_Valheim/5.4.2333",
		"Neobotics-SlayerSkills",
		"Smoothbrain-Mining",
		"# a comment",
		"",
		"Smoothbrain-Ranching?",
	}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var manifest string
	for _, f := range zr.File {
		if f.Name == "export.r2x" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			manifest = string(b)
		}
	}

	for _, want := range []string{"denikson-BepInExPack_Valheim", "Neobotics-SlayerSkills", "Smoothbrain-Mining", "Smoothbrain-Ranching"} {
		if !strings.Contains(manifest, want) {
			t.Errorf("profile is missing %q:\n%s", want, manifest)
		}
	}
	if strings.Contains(manifest, "comment") {
		t.Errorf("comments must not become mods:\n%s", manifest)
	}
	if n := strings.Count(manifest, "name:"); n != 4 {
		t.Errorf("expected 4 mods, manifest has %d name: keys:\n%s", n, manifest)
	}
}

func TestResolveVersionsPinsBareFullNames(t *testing.T) {
	index := map[string]string{
		"Smoothbrain-Mining":     "1.1.6",
		"Neobotics-SlayerSkills": "1.2.0",
	}
	latest := func(n string) (string, bool) { v, ok := index[n]; return v, ok }

	got := ResolveVersions([]string{
		"Smoothbrain-Mining",
		"Neobotics-SlayerSkills?",
		"denikson/BepInExPack_Valheim/5.4.2333",
		"Unknown-Mod",
		"# comment",
		"",
	}, latest)

	want := []string{
		"Smoothbrain/Mining/1.1.6",
		"Neobotics/SlayerSkills/1.2.0",
		"denikson/BepInExPack_Valheim/5.4.2333",
		"Unknown-Mod",
		"# comment",
		"",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolvedVersionsReachTheProfile(t *testing.T) {
	entries := ResolveVersions([]string{"Smoothbrain-Mining"}, func(string) (string, bool) { return "1.1.6", true })
	data, err := Build("boppo", entries, nil)
	if err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	var manifest string
	for _, f := range zr.File {
		if f.Name == "export.r2x" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			manifest = string(b)
		}
	}
	if strings.Contains(manifest, "major: 0\n") && strings.Contains(manifest, "patch: 0") {
		t.Errorf("version still 0.0.0 — r2modman rejects those as 'not found on Thunderstore':\n%s", manifest)
	}
	for _, want := range []string{"Smoothbrain-Mining", "major: 1", "minor: 1", "patch: 6"} {
		if !strings.Contains(manifest, want) {
			t.Errorf("manifest missing %q:\n%s", want, manifest)
		}
	}
}

func TestBuildKeepsVersionedThunderstoreIDs(t *testing.T) {
	data, err := Build("boppo", []string{"blacks7ar-BowPlugin-1.8.7"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	var m string
	for _, f := range zr.File {
		if f.Name == "export.r2x" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			m = string(b)
		}
	}
	if !strings.Contains(m, "blacks7ar-BowPlugin") {
		t.Errorf("mod dropped from profile:\n%s", m)
	}
	if !strings.Contains(m, "major: 1") || !strings.Contains(m, "patch: 7") {
		t.Errorf("version must be parsed out, not left in the name:\n%s", m)
	}
}
