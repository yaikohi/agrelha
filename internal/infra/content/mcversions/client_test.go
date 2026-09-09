package mcversions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const sampleManifest = `{
  "latest": {"release": "26.2", "snapshot": "26.3-pre-2"},
  "versions": [
    {"id": "26.3-pre-2", "type": "snapshot"},
    {"id": "26.2", "type": "release"},
    {"id": "26.1.2", "type": "release"},
    {"id": "1.21.11", "type": "release"},
    {"id": "1.21.1", "type": "release"}
  ]
}`

func newTestClient(t *testing.T) (*Client, *int) {
	t.Helper()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleManifest))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL), &hits
}

func TestLatestAndReleases(t *testing.T) {
	c, _ := newTestClient(t)
	ctx := context.Background()

	if got := c.Latest(ctx); got != "26.2" {
		t.Fatalf("Latest() = %q, want 26.2", got)
	}

	rel := c.Releases(ctx, 0)
	want := []string{"26.2", "26.1.2", "1.21.11", "1.21.1"}
	if len(rel) != len(want) {
		t.Fatalf("Releases() = %v, want %v", rel, want)
	}
	for i := range want {
		if rel[i] != want[i] {
			t.Fatalf("Releases()[%d] = %q, want %q", i, rel[i], want[i])
		}
	}

	if got := c.Releases(ctx, 2); len(got) != 2 || got[0] != "26.2" {
		t.Fatalf("Releases(limit=2) = %v", got)
	}
}

func TestSnapshotsExcludedAndCached(t *testing.T) {
	c, hits := newTestClient(t)
	ctx := context.Background()

	for _, v := range c.Releases(ctx, 0) {
		if v == "26.3-pre-2" {
			t.Fatal("snapshot leaked into Releases()")
		}
	}
	if !c.IsRelease(ctx, "26.2") {
		t.Fatal("IsRelease(26.2) = false")
	}
	if c.IsRelease(ctx, "26.3-pre-2") {
		t.Fatal("IsRelease(snapshot) = true")
	}
	if *hits != 1 {
		t.Fatalf("manifest fetched %d times, want 1 (cached)", *hits)
	}
}

func TestFallbackWhenUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL)
	if got := c.Latest(context.Background()); got != FallbackLatest {
		t.Fatalf("Latest() = %q, want fallback %q", got, FallbackLatest)
	}
}

func TestCompareAcrossSchemes(t *testing.T) {
	if Compare("26.2", "1.21.1") <= 0 {
		t.Fatal("26.2 must sort above 1.21.1")
	}
	if Compare("26.2", "26.1.2") <= 0 {
		t.Fatal("26.2 must sort above 26.1.2")
	}
	if Compare("1.21.1", "1.21.1") != 0 {
		t.Fatal("equal versions must compare 0")
	}
	if !IsModern("26.1.2") || IsModern("1.21.11") {
		t.Fatal("IsModern misclassified")
	}
}
