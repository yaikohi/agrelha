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

func TestMCVersions_EdgeCases(t *testing.T) {
	ctx := context.Background()

	// New("")
	cDefault := New("")
	if cDefault.url != DefaultManifestURL {
		t.Errorf("New(\"\") url = %q, want %q", cDefault.url, DefaultManifestURL)
	}

	// Server failure with no cache
	srvFail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srvFail.Close()

	cFail := New(srvFail.URL)
	rel := cFail.Releases(ctx, 10)
	if len(rel) != 1 || rel[0] != FallbackLatest {
		t.Errorf("Releases on failure = %v, want [%s]", rel, FallbackLatest)
	}
	if cFail.IsRelease(ctx, "26.2") {
		t.Error("IsRelease on failure should return false")
	}

	// Server success then failure returns stale cache
	var status int = http.StatusOK
	srvFlaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(sampleManifest))
		}
	}))
	defer srvFlaky.Close()

	cFlaky := New(srvFlaky.URL)
	cFlaky.ttl = 0 // always expired so it re-fetches
	if got := cFlaky.Latest(ctx); got != "26.2" {
		t.Fatalf("first fetch got %s, want 26.2", got)
	}
	// Upstream now fails
	status = http.StatusInternalServerError
	if got := cFlaky.Latest(ctx); got != "26.2" {
		t.Errorf("stale cache fetch got %s, want 26.2", got)
	}

	// Invalid JSON with stale cache
	status = http.StatusOK
	srvBadJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srvBadJSON.Close()
	cFlaky.url = srvBadJSON.URL
	if got := cFlaky.Latest(ctx); got != "26.2" {
		t.Errorf("stale cache on bad JSON got %s, want 26.2", got)
	}

	// Compare edge cases with invalid version numbers
	if Compare("invalid", "1.21.1") >= 0 {
		t.Error("invalid version should sort lower")
	}
	if Compare("1.21.1", "invalid") <= 0 {
		t.Error("valid version should sort higher")
	}
}

func TestMCVersions_AdditionalBranches(t *testing.T) {
	ctx := context.Background()

	// 1. splitVersion with suffix
	if parts := splitVersion("1.21.1-rc1"); len(parts) != 3 || parts[2] != 1 {
		t.Errorf("splitVersion with dash failed: %v", parts)
	}
	if parts := splitVersion("1.21.1+build"); len(parts) != 3 {
		t.Errorf("splitVersion with plus failed: %v", parts)
	}

	// 2. Manifest with no release versions
	srvSnapshotsOnly := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"latest": {"release": "", "snapshot": "26.3-pre-2"},
			"versions": [{"id": "26.3-pre-2", "type": "snapshot"}]
		}`))
	}))
	defer srvSnapshotsOnly.Close()

	cSnap := New(srvSnapshotsOnly.URL)
	releases := cSnap.Releases(ctx, 0)
	if len(releases) != 1 || releases[0] != FallbackLatest {
		t.Errorf("expected fallback when no releases: %v", releases)
	}

	// 3. Network transport error with stale cache
	srvClose := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleManifest))
	}))
	cTransport := New(srvClose.URL)
	cTransport.ttl = 0
	if got := cTransport.Latest(ctx); got != "26.2" {
		t.Fatalf("first fetch got %s", got)
	}
	srvClose.Close() // Close server so Do(req) returns transport error
	if got := cTransport.Latest(ctx); got != "26.2" {
		t.Errorf("expected stale cache on transport error, got %s", got)
	}

	// 4. Bad request URL error in load
	cBadURL := New("http://invalid url with spaces")
	if got := cBadURL.Latest(ctx); got != FallbackLatest {
		t.Errorf("expected fallback on bad url, got %s", got)
	}
}

