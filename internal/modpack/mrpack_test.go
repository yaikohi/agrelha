package modpack

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"agrelha/internal/modrinth"
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

func (m *mockModrinth) GetProjectVersions(ctx context.Context, idOrSlug, mcVersion string) ([]modrinth.Version, error) {
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
							URL:      "https://cdn.modrinth.com/data/xyz/ferritecore.jar",
							Primary:  true,
							Size:     654321,
							Hashes: map[string]string{
								"sha1":   "fchash123",
								"sha512": "fchash512",
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

	zipBytes, err := BuildMrpack(context.Background(), mock, "TestPack", "1.21.1", "21.1.249", slugs, configs)
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
