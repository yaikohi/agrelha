package modpackindex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModpackIndexClient(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/modpacks":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"data": [
					{"id": 85233, "name": "All the Mods 10", "slug": "atm10", "download_count": 500000}
				],
				"meta": {"current_page": 1, "last_page": 10, "per_page": 25, "total": 250}
			}`))
		case "/modpack/85233":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"data": {
					"id": 85233,
					"name": "All the Mods 10",
					"slug": "atm10",
					"minecraft_versions": [{"id": 1, "name": "1.21.1", "slug": "1-21-1"}]
				}
			}`))
		case "/modpack/85233/mods":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"data": [
					{
						"id": 1,
						"name": "Just Enough Items",
						"slug": "jei",
						"modrinth_info": [{"project_id": "u6dRKJwZ", "slug": "jei", "loaders": ["neoforge"]}]
					}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := New(ts.URL)

	// Test Search
	res, err := c.SearchModpacks(context.Background(), "atm10", "1.21.1", 1)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(res.Data) != 1 || res.Data[0].ID != 85233 {
		t.Fatalf("unexpected search response: %+v", res)
	}

	// Test Details
	detail, err := c.GetModpack(context.Background(), 85233)
	if err != nil {
		t.Fatalf("get details failed: %v", err)
	}
	if detail.Name != "All the Mods 10" || len(detail.MinecraftVersions) == 0 || detail.MinecraftVersions[0].Name != "1.21.1" {
		t.Fatalf("unexpected details: %+v", detail)
	}

	// Test Mods
	mods, err := c.GetModpackMods(context.Background(), 85233)
	if err != nil {
		t.Fatalf("get mods failed: %v", err)
	}
	if len(mods) != 1 || mods[0].Slug != "jei" {
		t.Fatalf("unexpected mods: %+v", mods)
	}
}

func TestModpackIndexVersionsAndErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/minecraft/versions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"data": [
					{"id": 91, "name": "1.21.1", "slug": "1-21-1"},
					{"id": 84, "name": "1.20.1", "slug": "1-20-1"}
				]
			}`))
		case "/minecraft/version/91/modpacks":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data": [{"id": 100, "name": "Version Pack"}], "meta": {"total": 1}}`))
		case "/modpacks":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data": [{"id": 200, "name": "Empty Search Pack"}], "meta": {"total": 1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	// Default baseURL
	cDef := New("")
	if cDef.baseURL != DefaultBaseURL {
		t.Errorf("expected default baseURL, got %s", cDef.baseURL)
	}

	c := New(ts.URL)

	// 1. Versions
	vers := c.Versions(context.Background())
	if len(vers) != 2 || vers[0].Name != "1.21.1" {
		t.Fatalf("unexpected versions: %+v", vers)
	}
	// Cached call
	versCached := c.Versions(context.Background())
	if len(versCached) != 2 {
		t.Fatalf("unexpected cached versions: %+v", versCached)
	}

	// 2. VersionIDs
	vIDs := c.VersionIDs(context.Background())
	if vIDs["1.21.1"] != 91 || vIDs["1.20.1"] != 84 {
		t.Fatalf("unexpected version IDs: %+v", vIDs)
	}

	// 3. Search by mcVersion without query
	resMC, err := c.SearchModpacks(context.Background(), "", "1.21.1", 1)
	if err != nil || len(resMC.Data) != 1 || resMC.Data[0].ID != 100 {
		t.Fatalf("SearchModpacks by mcVersion failed: %v, res: %+v", err, resMC)
	}

	// 4. Search by unknown mcVersion (falls back to /modpacks)
	resUnknown, err := c.SearchModpacks(context.Background(), "", "9.99.99", 0)
	if err != nil || len(resUnknown.Data) != 1 || resUnknown.Data[0].ID != 200 {
		t.Fatalf("SearchModpacks by unknown mcVersion failed: %v, res: %+v", err, resUnknown)
	}

	// 5. Search without query or mcVersion
	resEmpty, err := c.SearchModpacks(context.Background(), "", "", 0)
	if err != nil || len(resEmpty.Data) != 1 || resEmpty.Data[0].ID != 200 {
		t.Fatalf("SearchModpacks empty failed: %v, res: %+v", err, resEmpty)
	}

	// 6. Error handling
	cErr := New("http://invalid.invalid")
	if _, err := cErr.GetModpack(context.Background(), 1); err == nil {
		t.Error("expected error for invalid host")
	}
	if _, err := cErr.GetModpackMods(context.Background(), 1); err == nil {
		t.Error("expected error for invalid host")
	}
	if _, err := cErr.SearchModpacks(context.Background(), "q", "", 1); err == nil {
		t.Error("expected error for invalid host in SearchModpacks")
	}

	// 7. VersionIDs fallback to KnownMCVersionIDs when endpoint fails
	vIDsFallback := cErr.VersionIDs(context.Background())
	if vIDsFallback["1.21.1"] != KnownMCVersionIDs["1.21.1"] {
		t.Errorf("expected fallback to KnownMCVersionIDs, got %+v", vIDsFallback)
	}

	// 8. Non-200 status in getJSON
	tsErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer tsErr.Close()
	c500 := New(tsErr.URL)
	if _, err := c500.GetModpack(context.Background(), 1); err == nil {
		t.Error("expected error on 500 response")
	}
}

func TestAnalyzeLoader_NeoForgeForgeCompat(t *testing.T) {
	mods := []Mod{
		{
			ID:   1,
			Name: "Forge Mod",
			Slug: "forge-mod",
			ModrinthInfo: []ModrinthProjectRef{
				{ProjectID: "pid1", Slug: "forge-mod", Loaders: []string{"forge"}},
			},
		},
	}

	fit := AnalyzeLoader(mods, "neoforge")
	if fit.Supported != 1 || fit.Percent() != 100 {
		t.Errorf("expected neoforge to support forge mod, got %+v", fit)
	}
}

