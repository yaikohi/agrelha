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
