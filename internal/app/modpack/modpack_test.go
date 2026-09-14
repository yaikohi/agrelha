package modpack

import (
	"archive/zip"
	"bytes"
	"fmt"
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

type mockZipWriter struct {
	createErr  error
	closeErr   error
	writeErr   error
	failConfig bool
}

type mockWriter struct {
	err error
}

func (m mockWriter) Write(p []byte) (int, error) {
	if m.err != nil {
		return 0, m.err
	}
	return len(p), nil
}

func (m *mockZipWriter) Create(name string) (io.Writer, error) {
	if m.createErr != nil {
		if !m.failConfig || strings.Contains(name, "config/") {
			return nil, m.createErr
		}
	}
	if m.writeErr != nil {
		if !m.failConfig || strings.Contains(name, "config/") {
			return mockWriter{err: m.writeErr}, nil
		}
	}
	return mockWriter{}, nil
}

func (m *mockZipWriter) Close() error {
	return m.closeErr
}

func TestBuildErrors(t *testing.T) {
	origYaml := yamlMarshal
	origZip := newZipWriter
	defer func() {
		yamlMarshal = origYaml
		newZipWriter = origZip
	}()

	// 1. yamlMarshal error
	yamlMarshal = func(v any) ([]byte, error) {
		return nil, fmt.Errorf("yaml fail")
	}
	if _, err := Build("test", nil, nil); err == nil {
		t.Errorf("expected error when yamlMarshal fails")
	}
	yamlMarshal = origYaml

	// 2. zip Create("export.r2x") error
	newZipWriter = func(io.Writer) zipWriter {
		return &mockZipWriter{createErr: fmt.Errorf("create fail")}
	}
	if _, err := Build("test", nil, nil); err == nil {
		t.Errorf("expected error when Create export.r2x fails")
	}

	// 3. zip Write("export.r2x") error
	newZipWriter = func(io.Writer) zipWriter {
		return &mockZipWriter{writeErr: fmt.Errorf("write fail")}
	}
	if _, err := Build("test", nil, nil); err == nil {
		t.Errorf("expected error when Write export.r2x fails")
	}

	configs := map[string]string{"test.cfg": "data"}

	// 4. zip Create("config/...") error
	newZipWriter = func(io.Writer) zipWriter {
		return &mockZipWriter{createErr: fmt.Errorf("config create fail"), failConfig: true}
	}
	if _, err := Build("test", nil, configs); err == nil {
		t.Errorf("expected error when Create config fails")
	}

	// 5. zip Write("config/...") error
	newZipWriter = func(io.Writer) zipWriter {
		return &mockZipWriter{writeErr: fmt.Errorf("config write fail"), failConfig: true}
	}
	if _, err := Build("test", nil, configs); err == nil {
		t.Errorf("expected error when Write config fails")
	}

	// 6. zip Close error
	newZipWriter = func(io.Writer) zipWriter {
		return &mockZipWriter{closeErr: fmt.Errorf("close fail")}
	}
	if _, err := Build("test", nil, configs); err == nil {
		t.Errorf("expected error when Close fails")
	}
}

func TestParseEntryAndVersionEdgeCases(t *testing.T) {
	// 1. parseEntry
	badEntries := []string{
		"",
		"# comment",
		"noslashnohyphen",
		"/name/1.0.0",
		"ns//1.0.0",
	}
	for _, e := range badEntries {
		if _, ok := parseEntry(e); ok {
			t.Errorf("expected parseEntry(%q) to return false", e)
		}
	}

	// parseEntry hyphen with version vs bare
	m1, ok1 := parseEntry("ns-name-1.2.3")
	if !ok1 || m1.Name != "ns-name" || m1.Version.Major != 1 || m1.Version.Patch != 3 {
		t.Errorf("unexpected m1: %+v, ok: %v", m1, ok1)
	}

	m2, ok2 := parseEntry("ns-name")
	if !ok2 || m2.Name != "ns-name" {
		t.Errorf("unexpected m2: %+v, ok: %v", m2, ok2)
	}

	// 2. parseVersion with short fields
	vShort := parseVersion("1.2")
	if vShort.Major != 1 || vShort.Minor != 2 || vShort.Patch != 0 {
		t.Errorf("unexpected vShort: %+v", vShort)
	}

	// 3. ResolveVersions edge cases
	if res := ResolveVersions([]string{"foo"}, nil); len(res) != 1 || res[0] != "foo" {
		t.Errorf("expected passthrough when latest is nil")
	}
	res2 := ResolveVersions([]string{
		"", "# comment", "ns/name/1.0.0", "-mod", "mod-", "unfound-mod",
	}, func(string) (string, bool) {
		return "", false
	})
	if len(res2) != 6 {
		t.Errorf("expected 6 entries preserved, got %v", res2)
	}

	// 4. looksLikeVersion edge cases
	if looksLikeVersion("1.0") {
		t.Errorf("expected false for 1.0 (not 3 parts)")
	}
	if looksLikeVersion("1.0.beta") {
		t.Errorf("expected false for 1.0.beta (non-integer)")
	}
	if !looksLikeVersion("1.0.0") {
		t.Errorf("expected true for 1.0.0")
	}
}
