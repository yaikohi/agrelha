package thunderstore

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestThunderstoreClient(t *testing.T) {
	mux := http.NewServeMux()

	// 1. /package/ endpoint for warm()
	mux.HandleFunc("/package/", func(w http.ResponseWriter, r *http.Request) {
		packages := []rawPackage{
			{
				Name:         "Mining",
				Owner:        "Smoothbrain",
				PackageURL:   "https://thunderstore.io/p/Smoothbrain/Mining",
				IsDeprecated: false,
				Downloads:    5000,
				DateUpdated:  "2026-01-01T12:00:00Z",
				Versions: []struct {
					Description   string `json:"description"`
					Icon          string `json:"icon"`
					VersionNumber string `json:"version_number"`
				}{
					{
						Description:   "Enhanced mining",
						Icon:          "https://example.com/icon.png",
						VersionNumber: "1.2.0",
					},
				},
			},
			{
				Name:         "Jotunn",
				Owner:        "ValheimModding",
				PackageURL:   "https://thunderstore.io/p/ValheimModding/Jotunn",
				IsDeprecated: false,
				Downloads:    10000,
				DateUpdated:  "2026-01-02T12:00:00Z",
				Versions: []struct {
					Description   string `json:"description"`
					Icon          string `json:"icon"`
					VersionNumber string `json:"version_number"`
				}{
					{
						Description:   "Jotunn the Valheim Library",
						Icon:          "https://example.com/jotunn.png",
						VersionNumber: "2.20.0",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(packages)
	})

	// 2. Experimental endpoints
	mux.HandleFunc("/api/experimental/package/Smoothbrain/Mining/", func(w http.ResponseWriter, r *http.Request) {
		resp := expPackage{
			Namespace: "Smoothbrain",
			Name:      "Mining",
			Latest: expVersion{
				VersionNumber: "1.2.0",
				Dependencies:  []string{"denikson-BepInExPack_Valheim-5.4.2202", "ValheimModding-Jotunn-2.20.0"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/experimental/package/Smoothbrain/Mining/1.2.0/", func(w http.ResponseWriter, r *http.Request) {
		resp := expVersion{
			VersionNumber: "1.2.0",
			Dependencies:  []string{"denikson-BepInExPack_Valheim-5.4.2202", "ValheimModding-Jotunn-2.20.0"},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/experimental/package/ValheimModding/Jotunn/", func(w http.ResponseWriter, r *http.Request) {
		resp := expPackage{
			Namespace: "ValheimModding",
			Name:      "Jotunn",
			Latest: expVersion{
				VersionNumber: "2.20.0",
				Dependencies:  []string{},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/experimental/package/ValheimModding/Jotunn/2.20.0/", func(w http.ResponseWriter, r *http.Request) {
		resp := expVersion{
			VersionNumber: "2.20.0",
			Dependencies:  []string{},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/experimental/package/Smoothbrain/Mining/1.2.0/readme/", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{"markdown": "# Mining Mod"}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/experimental/package/Missing/Mod/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	c.expURL = srv.URL + "/api/experimental"
	c.http = srv.Client()

	ctx := context.Background()

	// 1. Ready & Preload
	if c.Ready() {
		t.Errorf("expected not ready initially")
	}

	refreshCalled := false
	c.OnRefresh = func(res []SearchResult) {
		refreshCalled = true
	}

	// 2. warm()
	if err := c.warm(ctx); err != nil {
		t.Fatalf("warm failed: %v", err)
	}
	if !c.Ready() {
		t.Errorf("expected ready after warm")
	}
	if !refreshCalled {
		t.Errorf("expected OnRefresh to be called")
	}

	// Check sorting (Jotunn 10000 downloads should be first)
	m1, ok := c.Get("ValheimModding/Jotunn")
	if !ok || m1.Downloads != 10000 {
		t.Errorf("Get Jotunn failed: ok=%v, res=%v", ok, m1)
	}

	// 3. Search
	results, err := c.Search(ctx, "mining", 10)
	if err != nil || len(results) != 1 || results[0].Name != "Mining" {
		t.Errorf("Search mining = %v, err=%v", results, err)
	}

	allResults, err := c.Search(ctx, "", 1)
	if err != nil || len(allResults) != 1 {
		t.Errorf("Search limit = %v, err=%v", allResults, err)
	}

	// 4. LatestVersion
	ver, deps, err := c.LatestVersion(ctx, "Smoothbrain", "Mining")
	if err != nil || ver != "1.2.0" || len(deps) != 2 {
		t.Errorf("LatestVersion failed: ver=%s, deps=%v, err=%v", ver, deps, err)
	}

	var nilClient *Client
	if v, d, err := nilClient.LatestVersion(ctx, "a", "b"); v != "" || d != nil || err != nil {
		t.Errorf("nil LatestVersion unexpected")
	}

	// 5. Readme
	readme, err := c.Readme(ctx, "Smoothbrain", "Mining", "1.2.0")
	if err != nil || readme != "# Mining Mod" {
		t.Errorf("Readme failed: %s, err=%v", readme, err)
	}
	if rm, err := nilClient.Readme(ctx, "a", "b", "1.0"); rm != "" || err != nil {
		t.Errorf("nil Readme unexpected")
	}

	// 6. ResolveTree
	tree, err := c.ResolveTree(ctx, "Smoothbrain", "Mining")
	if err != nil {
		t.Fatalf("ResolveTree failed: %v", err)
	}
	if len(tree) != 2 {
		t.Errorf("expected 2 resolved mods, got %v", tree)
	}

	// 7. ErrNotFound
	_, _, err = c.LatestVersion(ctx, "Missing", "Mod")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}

	// 8. Preload
	customIdx := []SearchResult{
		{Owner: "Custom", Name: "Mod", Version: "1.0.0"},
	}
	c.Preload(customIdx, time.Now())
	if m, ok := c.Get("Custom/Mod"); !ok || m.Owner != "Custom" {
		t.Errorf("Preload failed: %v", m)
	}
}

func TestThunderstoreEntry(t *testing.T) {
	// Normal entry
	line, skip := entry("Smoothbrain-Mining-1.2.0")
	if skip || line != "Smoothbrain/Mining/1.2.0" {
		t.Errorf("entry normal failed: %s, skip=%v", line, skip)
	}

	// BepInEx skip
	_, skip = entry("denikson-BepInExPack_Valheim-5.4.2202")
	if !skip {
		t.Errorf("expected BepInEx to be skipped")
	}

	// Too few parts
	_, skip = entry("invalid")
	if !skip {
		t.Errorf("expected invalid to be skipped")
	}
}

func TestThunderstore_WarmLoop(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/package/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name": "test"}]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	c.OnRefresh = func([]SearchResult) {
		cancel()
	}

	c.WarmLoop(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.index == nil {
		t.Error("expected index to be non-nil after WarmLoop")
	}
}

func TestThunderstore_ResolveTree_EdgeCases(t *testing.T) {
	mux := http.NewServeMux()
	// Root package
	mux.HandleFunc("/api/experimental/package/Author/Diamond/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(expPackage{
			Namespace: "Author",
			Name:      "Diamond",
			Latest: expVersion{
				VersionNumber: "1.0.0",
				Dependencies: []string{
					"Author-DepA-1.0.0",
					"Author-DepB-1.0.0",
					"ShortDep-1.0", // fewer than 3 parts
				},
			},
		})
	})
	mux.HandleFunc("/api/experimental/package/Author/Diamond/1.0.0/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(expVersion{
			VersionNumber: "1.0.0",
			Dependencies: []string{
				"Author-DepA-1.0.0",
				"Author-DepB-1.0.0",
				"ShortDep-1.0",
			},
		})
	})
	// DepA depends on Shared
	mux.HandleFunc("/api/experimental/package/Author/DepA/1.0.0/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(expVersion{
			VersionNumber: "1.0.0",
			Dependencies:  []string{"Author-Shared-1.0.0"},
		})
	})
	// DepB also depends on Shared (diamond dependency!)
	mux.HandleFunc("/api/experimental/package/Author/DepB/1.0.0/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(expVersion{
			VersionNumber: "1.0.0",
			Dependencies:  []string{"Author-Shared-1.0.0"},
		})
	})
	// Shared has no deps
	mux.HandleFunc("/api/experimental/package/Author/Shared/1.0.0/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(expVersion{
			VersionNumber: "1.0.0",
			Dependencies:  nil,
		})
	})
	// Failing package
	mux.HandleFunc("/api/experimental/package/Author/FailDep/1.0.0/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	c.expURL = srv.URL + "/api/experimental"
	c.http = srv.Client()

	// 1. Diamond dependency resolution + short dep skipping
	res, err := c.ResolveTree(context.Background(), "Author", "Diamond")
	if err != nil {
		t.Fatalf("ResolveTree diamond failed: %v", err)
	}
	if len(res) < 3 {
		t.Errorf("expected at least 3 resolved dependencies, got %v", res)
	}

	// 2. ResolveTree version dep failure
	mux.HandleFunc("/api/experimental/package/Author/Broken/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(expPackage{
			Namespace: "Author",
			Name:      "Broken",
			Latest: expVersion{
				VersionNumber: "1.0.0",
			},
		})
	})
	mux.HandleFunc("/api/experimental/package/Author/Broken/1.0.0/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})
	if _, err := c.ResolveTree(context.Background(), "Author", "Broken"); err == nil {
		t.Error("expected ResolveTree to fail on version fetch error")
	}
}

func TestThunderstore_Errors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/package/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})
	mux.HandleFunc("/badjson/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not json`))
	})
	mux.HandleFunc("/badtoken/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{invalid-token`))
	})
	mux.HandleFunc("/baditem/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"versions": "not-a-slice"}]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	c.expURL = srv.URL + "/api/experimental"
	c.http = srv.Client()

	ctx := context.Background()

	// warm fails on 500
	if err := c.warm(ctx); err == nil {
		t.Fatal("expected warm error on 500, got nil")
	}

	// getJSON fails on 500
	var v any
	if err := c.getJSON(ctx, srv.URL+"/package/", &v); err == nil {
		t.Fatal("expected error on 500 in getJSON, got nil")
	}

	// getJSON fails on bad json
	if err := c.getJSON(ctx, srv.URL+"/badjson/", &v); err == nil {
		t.Fatal("expected json decode error, got nil")
	}

	// warm fails on bad token
	c.v1URL = srv.URL + "/badtoken"
	if err := c.warm(ctx); err == nil {
		t.Fatal("expected warm error on bad token, got nil")
	}

	// warm fails on bad item
	c.v1URL = srv.URL + "/baditem"
	if err := c.warm(ctx); err == nil {
		t.Fatal("expected warm error on bad item, got nil")
	}
}

