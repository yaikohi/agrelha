package modrinth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"agrelha/internal/domain"
)

func TestModrinthClient(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"hits": [
					{"slug": "jei", "title": "Just Enough Items", "downloads": 1000000}
				],
				"total_hits": 1
			}`))
		case "/project/jei":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id": "u6dAmmoC", "slug": "jei", "title": "Just Enough Items"}`))
		case "/project/jei/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{
					"id": "v1",
					"version_number": "19.21.0.246",
					"dependencies": [
						{"project_id": "architectury", "dependency_type": "required"}
					]
				}
			]`))
		case "/project/architectury":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id": "lhGA9TYQ", "slug": "architectury-api", "title": "Architectury API"}`))
		case "/project/architectury-api/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{
					"id": "v-arch-1",
					"version_number": "13.0.8",
					"dependencies": []
				}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := New(ts.URL)

	// Test Search
	res, err := c.Search(context.Background(), "jei", "1.21.1", "neoforge", 10, 0)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Slug != "jei" {
		t.Fatalf("unexpected search result: %+v", res)
	}

	// Test Dependency Resolution
	deps, err := c.ResolveRequiredDependencies(context.Background(), "jei", "1.21.1", "neoforge")
	if err != nil {
		t.Fatalf("resolve dependencies failed: %v", err)
	}
	expectedDeps := []string{"architectury-api"}
	if !reflect.DeepEqual(deps, expectedDeps) {
		t.Fatalf("expected deps %v, got %v", expectedDeps, deps)
	}
}

func TestModrinthGetProjectsBatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"id": "p1", "slug": "mod-a", "title": "Mod A"},
				{"id": "p2", "slug": "mod-b", "title": "Mod B"}
			]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	c := New(ts.URL)
	projects, err := c.GetProjects(context.Background(), []string{"mod-a", "mod-b"})
	if err != nil {
		t.Fatalf("GetProjects failed: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
	if projects[0].Slug != "mod-a" || projects[1].Slug != "mod-b" {
		t.Errorf("unexpected projects returned: %+v", projects)
	}
}

func TestModrinthRetry429(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error": "rate limited"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": "p1", "slug": "test-mod", "title": "Test Mod"}`))
	}))
	defer ts.Close()

	c := New(ts.URL)
	p, err := c.GetProject(context.Background(), "test-mod")
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}
	if attempts < 2 {
		t.Errorf("expected at least 2 attempts, got %d", attempts)
	}
	if p.Slug != "test-mod" {
		t.Errorf("unexpected project: %+v", p)
	}
}

func TestModrinthErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/project/not-found":
			http.NotFound(w, r)
		case "/project/server-error":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal error"))
		case "/project/always-429":
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := New("")
	if c.baseURL != DefaultBaseURL {
		t.Errorf("expected default base url, got %s", c.baseURL)
	}

	c = New(ts.URL)

	// 404 / ErrPackageNotFound
	_, err := c.GetProject(context.Background(), "not-found")
	if err == nil {
		t.Error("expected 404 error, got nil")
	}

	// 500 Server Error
	_, err = c.GetProject(context.Background(), "server-error")
	if err == nil {
		t.Error("expected 500 error, got nil")
	}

	// Empty GetProjects
	projs, err := c.GetProjects(context.Background(), nil)
	if err != nil || projs != nil {
		t.Errorf("expected nil projects, got %v, err: %v", projs, err)
	}

	// Context canceled during getJSON
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.GetProject(ctx, "any")
	if err == nil {
		t.Error("expected ctx cancel error, got nil")
	}
}

func TestModrinthFallbackVersions(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/project/fallback-mod/version":
			loaders := r.URL.Query().Get("loaders")
			gameVersions := r.URL.Query().Get("game_versions")
			if gameVersions == `["1.20.1"]` {
				// empty for exact
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[]`))
				return
			}
			if gameVersions == `["1.20"]` {
				// base version match
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[{"id": "v-base", "version_number": "1.0.0", "game_versions": ["1.20"]}]`))
				return
			}
			if gameVersions == "" && loaders != "" {
				// empty query returns all
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[{"id": "v-all", "version_number": "2.0.0", "game_versions": ["1.21.1"]}]`))
				return
			}
			http.NotFound(w, r)
		case "/version/v123":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id": "v123", "version_number": "1.2.3"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := New(ts.URL)

	// Test point release fallback to base version
	vers, err := c.GetProjectVersions(context.Background(), "fallback-mod", "1.20.1", "neoforge")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vers) != 1 || vers[0].ID != "v-base" {
		t.Fatalf("expected v-base, got %+v", vers)
	}

	// Test GetVersion
	ver, err := c.GetVersion(context.Background(), "v123")
	if err != nil {
		t.Fatalf("GetVersion failed: %v", err)
	}
	if ver.ID != "v123" || ver.VersionNum != "1.2.3" {
		t.Fatalf("unexpected version: %+v", ver)
	}
}

func TestModrinthLatestAndInstance(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/project/test-mod/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{
					"id": "v1",
					"version_number": "2.5.0",
					"dependencies": [
						{"project_id": "dep-proj", "dependency_type": "required"}
					]
				}
			]`))
		case "/project/dep-proj":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id": "dep-id", "slug": "dep-slug", "title": "Dep Mod"}`))
		case "/project/dep-slug/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id": "v-dep", "version_number": "1.0.0", "dependencies": []}]`))
		case "/project/empty-mod/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := New(ts.URL)

	// LatestVersion
	verNum, _, err := c.LatestVersion(context.Background(), "", "test-mod")
	if err != nil || verNum != "2.5.0" {
		t.Fatalf("expected 2.5.0, got %s, err: %v", verNum, err)
	}

	// ResolveTree
	tree, err := c.ResolveTree(context.Background(), "test-mod", "")
	if err != nil || len(tree) != 1 || tree[0] != "dep-slug" {
		t.Fatalf("expected [dep-slug], got %v, err: %v", tree, err)
	}

	// LatestVersionForInstance
	inst := domain.Instance{
		Loader:    domain.LoaderNeoForge,
		MCVersion: "1.21.1",
	}
	ref := domain.ModRef{Name: "test-mod"}
	ver, deps, err := c.LatestVersionForInstance(context.Background(), ref, inst)
	if err != nil || ver != "2.5.0" || len(deps) != 1 || deps[0] != "dep-slug" {
		t.Fatalf("LatestVersionForInstance failed: ver=%s deps=%v err=%v", ver, deps, err)
	}

	// ResolveTreeForInstance
	treeInst, err := c.ResolveTreeForInstance(context.Background(), ref, inst)
	if err != nil || len(treeInst) != 1 || treeInst[0] != "dep-slug:1.0.0" {
		t.Fatalf("ResolveTreeForInstance failed: %v, err: %v", treeInst, err)
	}

	// Empty versions
	emptyRef := domain.ModRef{Namespace: "empty-mod"}
	ver, deps, err = c.LatestVersionForInstance(context.Background(), emptyRef, inst)
	if err != nil || ver != "" || len(deps) != 0 {
		t.Fatalf("expected empty for empty-mod, got ver=%s, deps=%v, err=%v", ver, deps, err)
	}
}
