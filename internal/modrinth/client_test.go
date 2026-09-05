package modrinth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
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

