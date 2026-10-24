package gmod

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type startedAtRuntime struct {
	mockRuntime
	startedAt time.Time
}

func (r *startedAtRuntime) Status(context.Context, ports.ServerRef) (ports.Status, error) {
	return ports.Status{Available: true, Lifecycle: ports.LifecycleRunning, StartedAt: r.startedAt}, nil
}

func (r *startedAtRuntime) Logs(context.Context, ports.ServerRef, ports.LogOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func restartPendingApp(t *testing.T, startedAt, collectionUpdated time.Time) *fiber.App {
	t.Helper()
	repo := &fakeInstanceRepo{items: make(map[int]domain.Instance)}
	_ = repo.Upsert(domain.Instance{
		GameID: domain.GameGMod, Number: 1, Name: "Spooky", Slug: "spooky",
		Tier: domain.TierSmall, State: domain.StateRunning,
		CreatedAt: time.Date(2026, 10, 24, 15, 50, 0, 0, time.UTC),
		GMod:      &domain.GModConfig{Pack: &domain.Pack{Provider: domain.ProviderSteamWorkshop, Ref: "2270097915"}},
	})

	mgr := instances.NewInstanceManager(
		repo, nil, &startedAtRuntime{startedAt: startedAt},
		24, 4, 2, "manifests/gmod", "192.168.20.226",
		&fakeRenderer{}, "gmod",
		instances.WithGameID(domain.GameGMod),
		instances.WithGameProfile(domain.GModProfile),
	)

	h := New(Config{
		Instances: mgr,
		Workshop: &mockWorkshop{col: domain.WorkshopCollection{
			ID: "2270097915", Title: "Lareira's House of Spooks", ItemCount: 297,
			TimeUpdated: collectionUpdated,
		}},
	})
	app := fiber.New()
	h.RegisterProtected(app)
	return app
}

func overviewBody(t *testing.T, app *fiber.App) string {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest("GET", "/gmod/1/overview", nil))
	if err != nil {
		t.Fatalf("GET overview: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overview status %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestRestartingTheServerClearsTheRestartPendingBanner(t *testing.T) {
	collectionUpdated := time.Date(2026, 10, 24, 16, 10, 0, 0, time.UTC)
	restartedAfter := collectionUpdated.Add(1 * time.Hour)

	body := overviewBody(t, restartPendingApp(t, restartedAfter, collectionUpdated))
	if strings.Contains(body, "Restart pending") {
		t.Error(`the server was restarted after the collection changed, yet the banner still demands a restart.

It compared the collection's upstream time against inst.CreatedAt, which never changes - so once the
collection was touched after the world was created, no restart could ever clear it and the operator
was told to restart a server they had just restarted.`)
	}
}

func TestACollectionChangedSinceTheServerStartedStillAsksForARestart(t *testing.T) {
	startedAt := time.Date(2026, 10, 24, 16, 0, 0, 0, time.UTC)
	collectionUpdated := startedAt.Add(30 * time.Minute)

	body := overviewBody(t, restartPendingApp(t, startedAt, collectionUpdated))
	if !strings.Contains(body, "Restart pending") {
		t.Error("the collection changed after the server started, so the banner must appear; otherwise the world silently runs stale addons")
	}
}

func TestAServerWithNoStartTimeIsNotToldToRestart(t *testing.T) {
	collectionUpdated := time.Date(2026, 10, 24, 16, 10, 0, 0, time.UTC)

	body := overviewBody(t, restartPendingApp(t, time.Time{}, collectionUpdated))
	if strings.Contains(body, "Restart pending") {
		t.Error("a world the runtime reports no start time for was told to restart; there is nothing running to restart")
	}
}
