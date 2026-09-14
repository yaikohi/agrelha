package minecraft

import (
	"context"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"agrelha/internal/infra/store"

	"github.com/gofiber/fiber/v2"
)

func searchApp(t *testing.T, fn SearchModsFunc) *fiber.App {
	t.Helper()
	h := New(Config{SearchMods: fn})
	app := fiber.New()
	app.Post("/api/minecraft/:num<int>/mods/search", h.MCInstanceModsSearch)
	return app
}

func post(t *testing.T, app *fiber.App, path, body string) string {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestModsSearchRendersResultsIntoThePage(t *testing.T) {
	app := searchApp(t, func(_ context.Context, q, _, _ string) ([]ModHit, error) {
		if q != "sodium" {
			t.Errorf("query not forwarded: %q", q)
		}
		return []ModHit{{Slug: "sodium", Title: "Sodium", Description: "Rendering engine", IconURL: "/i.png"}}, nil
	})

	out := post(t, app, "/api/minecraft/3/mods/search", `{"modQuery":"sodium"}`)

	if !strings.Contains(out, modResultsSelector) {
		t.Errorf("must patch %s so the page updates: %s", modResultsSelector, out)
	}
	for _, want := range []string{"Sodium", "Rendering engine", "/api/minecraft/3/mods/install"} {
		if !strings.Contains(out, want) {
			t.Errorf("result card missing %q", want)
		}
	}
}

func TestModsSearchEmptyQueryDoesNotCallProvider(t *testing.T) {
	called := false
	app := searchApp(t, func(context.Context, string, string, string) ([]ModHit, error) {
		called = true
		return nil, nil
	})

	post(t, app, "/api/minecraft/3/mods/search", `{"modQuery":"   "}`)
	if called {
		t.Error("a blank query must not hit Modrinth")
	}
}

func TestModsSearchSurvivesUnconfiguredProvider(t *testing.T) {
	app := searchApp(t, nil)
	out := post(t, app, "/api/minecraft/3/mods/search", `{"modQuery":"sodium"}`)
	if !strings.Contains(out, "unconfigured") {
		t.Errorf("must report unconfigured search rather than panic: %s", out)
	}
}

func TestModCardEscapesQuotesInSlug(t *testing.T) {
	card := modCardHTML(2, ModHit{Slug: "it's-a-mod", Title: "X"}, false)
	if strings.Contains(card, "{slug: 'it's") {
		t.Error("an unescaped quote would break the Datastar expression")
	}
}

func TestModsSearchPassesLoaderAndVersionFromInstance(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	repo := store.NewInstanceRepo(st)
	_ = repo.Upsert(domain.Instance{
		Number:    3,
		Slug:      "bob",
		Name:      "Bob",
		GameID:    domain.GameMinecraft,
		Loader:    domain.LoaderFabric,
		MCVersion: "26.2",
	})
	mgr := instances.NewInstanceManager(
		repo, nil, nil, 32, 8, 4,
		"manifests/minecraft-modded", "192.168.20.240", nil, "minecraft-modded",
		instances.WithGameID(domain.GameMinecraft),
	)

	var gotLoader, gotMCVersion string
	h := New(Config{
		MCInstances: mgr,
		SearchMods: func(_ context.Context, q, mcVersion, loader string) ([]ModHit, error) {
			gotMCVersion = mcVersion
			gotLoader = loader
			return []ModHit{{Slug: "cutter", Title: "Stonecutter"}}, nil
		},
	})
	app := fiber.New()
	app.Post("/api/minecraft/:num<int>/mods/search", h.MCInstanceModsSearch)

	out := post(t, app, "/api/minecraft/3/mods/search", `{"modQuery":"cutter"}`)
	if !strings.Contains(out, "Stonecutter") {
		t.Fatalf("expected Stonecutter in results: %s", out)
	}
	if gotLoader != "fabric" {
		t.Errorf("expected loader 'fabric', got %q", gotLoader)
	}
	if gotMCVersion != "26.2" {
		t.Errorf("expected mcVersion '26.2', got %q", gotMCVersion)
	}
}
