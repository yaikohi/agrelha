package curseforge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agrelha/internal/domain"
)

func packFilesServer(t *testing.T, files []any) (*httptest.Server, *string) {
	t.Helper()
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/mods/1136552/files") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		query = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"data": files})
	}))
	t.Cleanup(srv.Close)
	return srv, &query
}

func TestPackFactsReadsVersionAndLoaderFromTheNewestFile(t *testing.T) {
	srv, query := packFilesServer(t, []any{
		map[string]any{"id": 1, "fileDate": "2026-05-01T00:00:00Z", "gameVersions": []string{"26.1.2", "Fabric"}},
		map[string]any{"id": 2, "fileDate": "2026-10-27T14:54:44Z", "gameVersions": []string{"26.3", "Fabric"}},
	})

	facts, err := New(srv.URL, "key").PackFacts(context.Background(), 1136552)
	if err != nil {
		t.Fatal(err)
	}
	if facts.MCVersion != "26.3" {
		t.Errorf("Minecraft version = %q, want 26.3 from the newest file", facts.MCVersion)
	}
	if facts.Loader != domain.LoaderFabric {
		t.Errorf("loader = %q, want fabric", facts.Loader)
	}
	if strings.Contains(*query, "modLoaderType") {
		t.Errorf("the files must be fetched unfiltered or a Fabric pack disappears: %s", *query)
	}
}

func TestPackFactsIgnoresLoaderNamesWhenPickingTheVersion(t *testing.T) {
	srv, _ := packFilesServer(t, []any{
		map[string]any{"id": 1, "fileDate": "2026-10-27T00:00:00Z", "gameVersions": []string{"NeoForge", "1.21.1"}},
	})

	facts, err := New(srv.URL, "key").PackFacts(context.Background(), 1136552)
	if err != nil {
		t.Fatal(err)
	}
	if facts.MCVersion != "1.21.1" {
		t.Errorf("Minecraft version = %q; a loader name is not a version", facts.MCVersion)
	}
	if facts.Loader != domain.LoaderNeoForge {
		t.Errorf("loader = %q, want neoforge", facts.Loader)
	}
}

func TestPackFactsWithoutAnAPIKeyAsksNothing(t *testing.T) {
	if _, err := New("", "").PackFacts(context.Background(), 1136552); err == nil {
		t.Error("an unconfigured client must refuse rather than guess")
	}
}
