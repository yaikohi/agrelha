package curseforge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

// fakeCF serves the shape of the CurseForge v1 API we depend on.
func fakeCF(t *testing.T, mods map[int]map[string]any, files map[int][]map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/mods/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		slug := r.URL.Query().Get("slug")
		var out []any
		for _, m := range mods {
			if slug == "" || m["slug"] == slug {
				out = append(out, m)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
	})

	mux.HandleFunc("/mods/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/mods/")
		parts := strings.Split(rest, "/")
		id := 0
		_, _ = fmtSscan(parts[0], &id)
		if len(parts) >= 2 && parts[1] == "files" {
			f, ok := files[id]
			if !ok {
				f = []map[string]any{}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": f})
			return
		}
		m, ok := mods[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": m})
	})

	return httptest.NewServer(mux)
}

func fmtSscan(s string, v *int) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int(r-'0')
	}
	*v = n
	return 1, nil
}

func mod(id int, slug, name string, restricted *bool) map[string]any {
	m := map[string]any{"id": id, "slug": slug, "name": name, "summary": name + " does things"}
	if restricted != nil {
		m["allowModDistribution"] = *restricted
	}
	return m
}

func file(id int, date string, deps ...int) map[string]any {
	var d []any
	for _, m := range deps {
		d = append(d, map[string]any{"modId": m, "relationType": relationRequired})
	}
	return map[string]any{"id": id, "fileDate": date, "downloadUrl": "https://cdn/x.jar", "dependencies": d}
}

// A client with no key is "not configured", never a broken client: agrelha must
// keep working with Modrinth alone.
func TestNoKeyMeansNoClient(t *testing.T) {
	if c := New("https://api.curseforge.com/v1", ""); c != nil {
		t.Fatal("an empty key must yield a nil client, not a client that 403s on every call")
	}
	var c *Client
	if c.Ready() {
		t.Error("a nil client is never ready")
	}
	if got, err := c.Search(context.Background(), "jei", "1.21.1", domain.LoaderFabric, 5); got != nil || err != nil {
		t.Errorf("a nil client returns nothing quietly, got %v %v", got, err)
	}
}

func TestSearchFiltersByVersionAndLoader(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{mod(1, "jei", "JEI", nil)}})
	}))
	defer srv.Close()

	c := New(srv.URL, "k")
	out, err := c.Search(context.Background(), "jei", "1.21.1", domain.LoaderFabric, 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(out) != 1 || out[0].Slug != "jei" {
		t.Fatalf("unexpected results: %+v", out)
	}
	for _, want := range []string{"gameVersion=1.21.1", "modLoaderType=4", "classId=6"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q - an unfiltered search returns mods the world cannot load", gotQuery, want)
		}
	}
}

// ADR 0004: the whole reason agrelha resolves CurseForge dependencies itself.
func TestResolvePinsTheModAndEveryRequiredDependency(t *testing.T) {
	mods := map[int]map[string]any{
		1: mod(1, "jei", "JEI", nil),
		2: mod(2, "architectury", "Architectury", nil),
		3: mod(3, "cloth-config", "Cloth Config", nil),
	}
	files := map[int][]map[string]any{
		1: {file(111, "2026-01-02"), file(110, "2026-01-01")},
		2: {file(222, "2026-01-02", 3)},
		3: {file(333, "2026-01-02")},
	}
	srv := fakeCF(t, mods, files)
	defer srv.Close()

	// jei -> architectury -> cloth-config, and the newest file wins at each step.
	files[1] = []map[string]any{file(111, "2026-01-02", 2), file(110, "2026-01-01")}

	got, err := New(srv.URL, "k").Resolve(context.Background(), "jei", "1.21.1", "fabric")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := []string{"jei:111", "architectury:222", "cloth-config:333"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Resolve = %v, want %v (transitive, pinned, de-duplicated)", got, want)
	}
}

func TestResolveRefusesARestrictedMod(t *testing.T) {
	no := false
	srv := fakeCF(t,
		map[int]map[string]any{1: mod(1, "restricted", "Restricted Mod", &no)},
		map[int][]map[string]any{1: {file(111, "2026-01-02")}})
	defer srv.Close()

	_, err := New(srv.URL, "k").Resolve(context.Background(), "restricted", "1.21.1", "fabric")
	if !errors.Is(err, ErrRestricted) {
		t.Fatalf("want ErrRestricted, got %v", err)
	}
}

// A dependency the operator never chose can also be restricted, and that must
// fail at install rather than leaving a world that cannot boot.
func TestResolveRefusesARestrictedDependency(t *testing.T) {
	no := false
	srv := fakeCF(t,
		map[int]map[string]any{
			1: mod(1, "fine", "Fine Mod", nil),
			2: mod(2, "locked", "Locked Dep", &no),
		},
		map[int][]map[string]any{
			1: {file(111, "2026-01-02", 2)},
			2: {file(222, "2026-01-02")},
		})
	defer srv.Close()

	_, err := New(srv.URL, "k").Resolve(context.Background(), "fine", "1.21.1", "fabric")
	if !errors.Is(err, ErrRestricted) {
		t.Fatalf("want ErrRestricted for the dependency, got %v", err)
	}
}

// No build for this world is an install-time failure, not a boot-time one.
func TestResolveFailsWhenNoFileMatchesTheWorld(t *testing.T) {
	srv := fakeCF(t,
		map[int]map[string]any{1: mod(1, "jei", "JEI", nil)},
		map[int][]map[string]any{})
	defer srv.Close()

	_, err := New(srv.URL, "k").Resolve(context.Background(), "jei", "1.99.9", "fabric")
	if !errors.Is(err, ports.ErrPackageNotFound) {
		t.Fatalf("want ErrPackageNotFound, got %v", err)
	}
}

// Absent allowModDistribution means allowed - CurseForge omits it for most mods,
// and treating absence as restricted would block nearly everything.
func TestAbsentDistributionFlagMeansAllowed(t *testing.T) {
	srv := fakeCF(t,
		map[int]map[string]any{1: mod(1, "jei", "JEI", nil)},
		map[int][]map[string]any{1: {file(111, "2026-01-02")}})
	defer srv.Close()

	m, err := New(srv.URL, "k").GetMod(context.Background(), "jei")
	if err != nil {
		t.Fatalf("GetMod: %v", err)
	}
	if m.Restricted {
		t.Error("a mod with no flag must be installable")
	}
}
