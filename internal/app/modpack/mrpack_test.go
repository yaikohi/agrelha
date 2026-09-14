package modpack

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agrelha/internal/infra/content/modrinth"
)

type mockModrinth struct {
	projects map[string]*modrinth.Project
	versions map[string][]modrinth.Version
}

func (m *mockModrinth) GetProject(ctx context.Context, idOrSlug string) (*modrinth.Project, error) {
	if p, ok := m.projects[idOrSlug]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("project not found: %s", idOrSlug)
}

func (m *mockModrinth) GetProjectVersions(ctx context.Context, idOrSlug, mcVersion, loader string) ([]modrinth.Version, error) {
	if v, ok := m.versions[idOrSlug]; ok {
		return v, nil
	}
	return nil, fmt.Errorf("versions not found: %s", idOrSlug)
}

func TestBuildMrpack(t *testing.T) {
	mock := &mockModrinth{
		projects: map[string]*modrinth.Project{
			"jei": {
				Slug:       "jei",
				ClientSide: "optional",
				ServerSide: "optional",
			},
			"luckperms": {
				Slug:       "luckperms",
				ClientSide: "unsupported", // Server-only
				ServerSide: "required",
			},
			"ferrite-core": {
				Slug:       "ferrite-core",
				ClientSide: "required",
				ServerSide: "optional",
			},
		},
		versions: map[string][]modrinth.Version{
			"jei": {
				{
					VersionNum: "19.21.0.246",
					Files: []modrinth.VersionFile{
						{
							FileName: "jei-1.21.1-neoforge-19.21.0.246.jar",
							URL:      "https://cdn.modrinth.com/data/u6dRKJwZ/versions/abc/jei.jar",
							Primary:  true,
							Size:     123456,
							Hashes: map[string]string{
								"sha1":   "hash123",
								"sha512": "hash512abc",
							},
						},
					},
				},
			},
			"ferrite-core": {
				{
					VersionNum: "7.0.0",
					Files: []modrinth.VersionFile{
						{
							FileName: "ferritecore-7.0.0-neoforge.jar",
							URL:      "https://cdn.modrinth.com/data/ferrite/versions/xyz/fc.jar",
							Primary:  true,
							Size:     654321,
							Hashes: map[string]string{
								"sha1":   "hash456",
								"sha512": "hash512xyz",
							},
						},
					},
				},
			},
		},
	}

	configs := map[string]string{
		"jei-client.ini": "showCheats = true\n",
		"mod.toml":       "key = \"value\"\n",
	}

	slugs := []string{"jei", "luckperms", "ferrite-core"}

	zipBytes, err := BuildMrpack(context.Background(), mock, "TestPack", "1.21.1", "neoforge", "21.1.249", slugs, configs)
	if err != nil {
		t.Fatalf("BuildMrpack failed: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}

	foundFiles := make(map[string][]byte)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open zip file %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read zip file %s: %v", f.Name, err)
		}
		foundFiles[f.Name] = data
	}

	// 1. Verify modrinth.index.json exists
	indexData, ok := foundFiles["modrinth.index.json"]
	if !ok {
		t.Fatal("modrinth.index.json missing from mrpack")
	}

	var idx MrpackIndex
	if err := json.Unmarshal(indexData, &idx); err != nil {
		t.Fatalf("unmarshal modrinth.index.json: %v", err)
	}

	if idx.Dependencies["minecraft"] != "1.21.1" {
		t.Errorf("minecraft version = %s, want 1.21.1", idx.Dependencies["minecraft"])
	}
	if idx.Dependencies["neoforge"] != "21.1.249" {
		t.Errorf("neoforge version = %s, want 21.1.249", idx.Dependencies["neoforge"])
	}

	// 2. Verify server-only mod luckperms is excluded, but jei and ferrite-core are included
	if len(idx.Files) != 2 {
		t.Fatalf("files count = %d, want 2", len(idx.Files))
	}

	for _, f := range idx.Files {
		if f.Path == "mods/luckperms.jar" {
			t.Errorf("luckperms was included, but is server-only!")
		}
	}

	// 3. Verify configs in overrides/config/
	if string(foundFiles["overrides/config/jei-client.ini"]) != "showCheats = true\n" {
		t.Errorf("overrides/config/jei-client.ini content mismatch")
	}
	if string(foundFiles["overrides/config/mod.toml"]) != "key = \"value\"\n" {
		t.Errorf("overrides/config/mod.toml content mismatch")
	}
}

func TestBuildMrpackFabric(t *testing.T) {
	mock := &mockModrinth{
		projects: map[string]*modrinth.Project{
			"fabric-api": {
				Slug:       "fabric-api",
				ClientSide: "optional",
				ServerSide: "optional",
			},
		},
		versions: map[string][]modrinth.Version{
			"fabric-api": {
				{
					VersionNum: "0.100.0",
					Files: []modrinth.VersionFile{
						{
							FileName: "fabric-api-0.100.0.jar",
							URL:      "https://cdn.modrinth.com/data/fabric/api.jar",
							Primary:  true,
							Size:     100000,
						},
					},
				},
			},
		},
	}

	zipBytes, err := BuildMrpack(context.Background(), mock, "FabricPack", "1.21.1", "fabric", "latest", []string{"fabric-api"}, nil)
	if err != nil {
		t.Fatalf("BuildMrpack failed: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}

	var foundIndex bool
	for _, f := range zr.File {
		if f.Name == "modrinth.index.json" {
			foundIndex = true
			rc, _ := f.Open()
			var idx MrpackIndex
			_ = json.NewDecoder(rc).Decode(&idx)
			rc.Close()
			if idx.Dependencies["fabric-loader"] != "0.16.10" {
				t.Errorf("fabric-loader version = %s, want 0.16.10", idx.Dependencies["fabric-loader"])
			}
			if idx.Dependencies["minecraft"] != "1.21.1" {
				t.Errorf("minecraft version = %s, want 1.21.1", idx.Dependencies["minecraft"])
			}
		}
	}
	if !foundIndex {
		t.Fatal("missing modrinth.index.json")
	}
}

func TestResolveNeoForgeVersion(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		mcVersion  string
		requested  string
		wantPrefix string
	}{
		{"1.20.1", "latest", "47.1."},
		{"1.20.1", "", "47.1."},
		{"1.20.4", "latest", "20.4."},
		{"1.20.6", "latest", "20.6."},
		{"1.21.1", "latest", "21.1."},
		{"1.21.1", "21.1.100", "21.1.100"}, // explicit requested version preserved
	}

	for _, tt := range tests {
		got := ResolveNeoForgeVersion(ctx, tt.mcVersion, tt.requested)
		if !strings.HasPrefix(got, tt.wantPrefix) {
			t.Errorf("ResolveNeoForgeVersion(%q, %q) = %q, want prefix %q", tt.mcVersion, tt.requested, got, tt.wantPrefix)
		}
	}
}

type mockBatchModrinth struct {
	mockModrinth
	batchErr error
}

func (m *mockBatchModrinth) GetProjects(ctx context.Context, idsOrSlugs []string) ([]modrinth.Project, error) {
	if m.batchErr != nil {
		return nil, m.batchErr
	}
	var out []modrinth.Project
	for _, id := range idsOrSlugs {
		if p, ok := m.projects[id]; ok {
			out = append(out, *p)
		}
	}
	return out, nil
}

func TestBuildMrpackWithReportAndBatch(t *testing.T) {
	mock := &mockBatchModrinth{
		mockModrinth: mockModrinth{
			projects: map[string]*modrinth.Project{
				"jei": {
					Slug:       "jei",
					ClientSide: "optional",
					ServerSide: "optional",
				},
				"luckperms": {
					Slug:       "luckperms",
					ClientSide: "unsupported", // Server-only
					ServerSide: "required",
				},
			},
			versions: map[string][]modrinth.Version{
				"jei": {
					{
						VersionNum: "19.21.0.246",
						Files: []modrinth.VersionFile{
							{
								FileName: "jei-1.20.1-neoforge-19.21.0.246.jar",
								URL:      "https://cdn.modrinth.com/data/jei.jar",
								Primary:  true,
								Size:     123456,
							},
						},
					},
				},
			},
		},
	}

	slugs := []string{"jei", "luckperms", "nonexistent-curseforge-mod"}

	zipBytes, err := BuildMrpack(context.Background(), mock, "Test120Pack", "1.20.1", "neoforge", "latest", slugs, nil)
	if err != nil {
		t.Fatalf("BuildMrpack failed: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}

	var foundIndex, foundReport bool
	for _, f := range zr.File {
		if f.Name == "modrinth.index.json" {
			foundIndex = true
			rc, _ := f.Open()
			var idx MrpackIndex
			_ = json.NewDecoder(rc).Decode(&idx)
			rc.Close()

			if idx.Dependencies["neoforge"] != "47.1.106" {
				t.Errorf("expected neoforge 47.1.106 for MC 1.20.1, got %s", idx.Dependencies["neoforge"])
			}
			if len(idx.Files) != 1 {
				t.Errorf("expected 1 file (jei), got %d", len(idx.Files))
			}
		}
		if f.Name == "overrides/MOD_EXPORT_REPORT.txt" {
			foundReport = true
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			content := string(b)
			if !strings.Contains(content, "luckperms") {
				t.Errorf("report should mention luckperms as server-only")
			}
			if !strings.Contains(content, "nonexistent-curseforge-mod") {
			}
		}
	}

	if !foundIndex {
		t.Error("modrinth.index.json not found")
	}
	if !foundReport {
		t.Error("overrides/MOD_EXPORT_REPORT.txt not found")
	}
}

func TestBuildMrpackLiveSample(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live Modrinth test in short mode")
	}

	mr := modrinth.New("")
	slugs := []string{
		"enchantment-descriptions",
		"xaeros-minimap",
		"curios",
		"aether",
		"create",
		"entityculling", // was failing before (only forge tagged)
		"placebo",
		"sodium",
		"spark", // server-only mod
	}

	zipBytes, err := BuildMrpack(context.Background(), mr, "LiveTestPack", "1.20.1", "neoforge", "latest", slugs, nil)
	if err != nil {
		t.Fatalf("BuildMrpack failed: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}

	var foundIndex, foundReport bool
	for _, f := range zr.File {
		if f.Name == "modrinth.index.json" {
			foundIndex = true
			rc, _ := f.Open()
			var idx MrpackIndex
			_ = json.NewDecoder(rc).Decode(&idx)
			rc.Close()

			if idx.Dependencies["neoforge"] != "47.1.106" {
				t.Errorf("expected neoforge 47.1.106, got %s", idx.Dependencies["neoforge"])
			}
			t.Logf("Exported %d client mod files", len(idx.Files))
		}
		if f.Name == "overrides/MOD_EXPORT_REPORT.txt" {
			foundReport = true
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			t.Logf("Export report:\n%s", string(b))
		}
	}

	if !foundIndex || !foundReport {
		t.Errorf("missing index or report: index=%v, report=%v", foundIndex, foundReport)
	}
}

func TestResolveNeoForgeVersionAllCases(t *testing.T) {
	ctx := context.Background()

	// 1. Explicit requested version
	if v := ResolveNeoForgeVersion(ctx, "1.21.1", "21.1.99"); v != "21.1.99" {
		t.Errorf("expected requested version 21.1.99, got %s", v)
	}

	origURL := prismMetaURL
	defer func() { prismMetaURL = origURL }()

	// 2. HTTP Server returning matching version
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"versions": [
				{
					"version": "21.1.888",
					"requires": [{"uid": "net.minecraft", "equals": "1.21.1"}]
				}
			]
		}`))
	}))
	defer ts.Close()

	prismMetaURL = ts.URL
	if v := ResolveNeoForgeVersion(ctx, "1.21.1", "latest"); v != "21.1.888" {
		t.Errorf("expected 21.1.888 from metadata, got %s", v)
	}

	// 3. HTTP Server returning non-matching version
	if v := ResolveNeoForgeVersion(ctx, "1.20.1", "recommended"); v != "47.1.106" {
		t.Errorf("expected fallback 47.1.106, got %s", v)
	}

	// 4. Fallback switch cases when HTTP fails
	prismMetaURL = "http://127.0.0.1:0/fail"
	cases := []struct {
		mcVersion string
		want      string
	}{
		{"1.21.1", "21.1.249"},
		{"1.21.0", "21.0.167"},
		{"1.21", "21.0.167"},
		{"1.20.6", "20.6.141"},
		{"1.20.4", "20.4.237"},
		{"1.20.2", "20.2.88"},
		{"1.20.1", "47.1.106"},
		{"1.20", "47.1.106"},
		{"1.20.5", "47.1.106"},
		{"1.19.4", "21.1.249"},
	}

	for _, c := range cases {
		if got := ResolveNeoForgeVersion(ctx, c.mcVersion, ""); got != c.want {
			t.Errorf("ResolveNeoForgeVersion(%s) = %s, want %s", c.mcVersion, got, c.want)
		}
	}
}

func TestBuildMrpackAllEdgeCases(t *testing.T) {
	ctx := context.Background()

	mock := &mockBatchModrinth{
		mockModrinth: mockModrinth{
			projects: map[string]*modrinth.Project{
				"opt-mod": {
					Slug:       "opt-mod",
					ClientSide: "optional",
					ServerSide: "optional",
				},
				"non-primary": {
					Slug:       "non-primary",
					ClientSide: "required",
					ServerSide: "unsupported",
				},
				"no-jar": {
					Slug:       "no-jar",
					ClientSide: "required",
				},
				"empty-url": {
					Slug:       "empty-url",
					ClientSide: "required",
				},
				"server-mod": {
					Slug:       "server-mod",
					ClientSide: "unsupported",
				},
			},
			versions: map[string][]modrinth.Version{
				"opt-mod": {
					{
						VersionNum: "1.0",
						Files: []modrinth.VersionFile{
							{FileName: "opt.jar", URL: "http://url", Primary: true},
						},
					},
				},
				"non-primary": {
					{
						VersionNum: "2.0",
						Files: []modrinth.VersionFile{
							{FileName: "fallback.jar", URL: "http://fallback", Primary: false},
						},
					},
				},
				"no-jar": {
					{
						VersionNum: "3.0",
						Files: []modrinth.VersionFile{
							{FileName: "readme.txt", URL: "http://txt", Primary: true},
						},
					},
				},
				"empty-url": {
					{
						VersionNum: "4.0",
						Files: []modrinth.VersionFile{
							{FileName: "empty.jar", URL: "", Primary: true},
						},
					},
				},
			},
		},
		batchErr: fmt.Errorf("batch failure"),
	}

	// 1. Default mcVersion and loaderType, duplicates in slugs, batch error fallback, non-primary jar, no jar, missing mod
	slugs := []string{
		"opt-mod", "opt-mod", "opt-mod?", "# comment", "",
		"non-primary", "no-jar", "empty-url", "server-mod", "missing-mod", "no-version-mod",
	}
	data, err := BuildMrpack(ctx, mock, "EdgePack", "", "", "", slugs, map[string]string{"foo.txt": "bar"})
	if err != nil {
		t.Fatalf("BuildMrpack failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected non-empty data")
	}

	// 2. Fabric loader with empty and custom loaderVersion, batch success
	mock.batchErr = nil
	fabricData, err := BuildMrpack(ctx, mock, "FabricPack", "1.21.1", "fabric", "", []string{"opt-mod"}, nil)
	if err != nil {
		t.Fatalf("BuildMrpack fabric failed: %v", err)
	}
	if len(fabricData) == 0 {
		t.Fatalf("expected non-empty fabricData")
	}

	fabricData2, err := BuildMrpack(ctx, mock, "FabricPack2", "1.21.1", "fabric", "0.15.0", []string{"opt-mod"}, nil)
	if err != nil {
		t.Fatalf("BuildMrpack fabric2 failed: %v", err)
	}
	if len(fabricData2) == 0 {
		t.Fatalf("expected non-empty fabricData2")
	}
}

func TestBuildMrpackZipErrors(t *testing.T) {
	ctx := context.Background()
	mock := &mockModrinth{
		projects: map[string]*modrinth.Project{
			"mod": {Slug: "mod"},
		},
		versions: map[string][]modrinth.Version{
			"mod": {{VersionNum: "1.0", Files: []modrinth.VersionFile{{FileName: "mod.jar", URL: "http://url", Primary: true}}}},
		},
	}

	origMarshal := mrpackJSONMarshalIndent
	origZip := mrpackNewZipWriter
	defer func() {
		mrpackJSONMarshalIndent = origMarshal
		mrpackNewZipWriter = origZip
	}()

	// 1. JSON marshal error
	mrpackJSONMarshalIndent = func(v any, prefix, indent string) ([]byte, error) {
		return nil, fmt.Errorf("marshal fail")
	}
	if _, err := BuildMrpack(ctx, mock, "Pack", "1.21.1", "neoforge", "", []string{"mod"}, nil); err == nil {
		t.Errorf("expected error when mrpackJSONMarshalIndent fails")
	}
	mrpackJSONMarshalIndent = origMarshal

	// 2. Create("modrinth.index.json") error
	mrpackNewZipWriter = func(io.Writer) zipWriter {
		return &mockZipWriter{createErr: fmt.Errorf("create index fail")}
	}
	if _, err := BuildMrpack(ctx, mock, "Pack", "1.21.1", "neoforge", "", []string{"mod"}, nil); err == nil {
		t.Errorf("expected error when Create index fails")
	}

	// 3. Write("modrinth.index.json") error
	mrpackNewZipWriter = func(io.Writer) zipWriter {
		return &mockZipWriter{writeErr: fmt.Errorf("write index fail")}
	}
	if _, err := BuildMrpack(ctx, mock, "Pack", "1.21.1", "neoforge", "", []string{"mod"}, nil); err == nil {
		t.Errorf("expected error when Write index fails")
	}

	configs := map[string]string{"test.cfg": "data"}

	// 4. Create("overrides/config/...") error
	mrpackNewZipWriter = func(io.Writer) zipWriter {
		return &mockZipWriter{createErr: fmt.Errorf("create config fail"), failConfig: true}
	}
	if _, err := BuildMrpack(ctx, mock, "Pack", "1.21.1", "neoforge", "", []string{"mod"}, configs); err == nil {
		t.Errorf("expected error when Create config fails")
	}

	// 5. Write("overrides/config/...") error
	mrpackNewZipWriter = func(io.Writer) zipWriter {
		return &mockZipWriter{writeErr: fmt.Errorf("write config fail"), failConfig: true}
	}
	if _, err := BuildMrpack(ctx, mock, "Pack", "1.21.1", "neoforge", "", []string{"mod"}, configs); err == nil {
		t.Errorf("expected error when Write config fails")
	}

	// 6. Close() error
	mrpackNewZipWriter = func(io.Writer) zipWriter {
		return &mockZipWriter{closeErr: fmt.Errorf("close fail")}
	}
	if _, err := BuildMrpack(ctx, mock, "Pack", "1.21.1", "neoforge", "", []string{"mod"}, configs); err == nil {
		t.Errorf("expected error when Close fails")
	}
}
