package dashboard

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

func TestDashboardPage(t *testing.T) {
	h := New(Config{
		GameNodeName:   "game-01",
		ValheimAddress: "192.168.20.224:2456",
	})

	app := fiber.New()
	h.Register(app)

	// 1. Visit dashboard as guest
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "game-01") {
		t.Errorf("expected node name game-01 in dashboard body")
	}
}

func TestTileSignals(t *testing.T) {
	h := New(Config{
		ModUpdateTotal:        func() int { return 5 },
		ValheimModUpdateTotal: func() int { return 2 },
		MCModUpdateTotal:      func() int { return 3 },
		BackupInfo: func() (BackupSummary, bool) {
			return BackupSummary{
				Count:      3,
				TotalSize:  1024 * 1024 * 50,
				LatestSize: 1024 * 1024 * 20,
				LatestAt:   time.Now().Add(-5 * time.Minute),
			}, true
		},
	})

	sig := h.TileSignals(context.Background())
	if sig["backupinfo"] == "" {
		t.Errorf("expected non-empty backupinfo signal")
	}
	if sig["backup"] == "—" {
		t.Errorf("expected human ago for backup, got '—'")
	}
	if sig["updates"] != 5 {
		t.Errorf("expected updates = 5, got %v", sig["updates"])
	}
	if sig["valheim_updates"] != 2 {
		t.Errorf("expected valheim_updates = 2, got %v", sig["valheim_updates"])
	}
	if sig["mc_updates"] != 3 {
		t.Errorf("expected mc_updates = 3, got %v", sig["mc_updates"])
	}
}

type fakeGameEngine struct {
	telemetry domain.GameTelemetry
}

func (f *fakeGameEngine) ID() domain.GameID                  { return "mock" }
func (f *fakeGameEngine) Display() domain.Display            { return domain.Display{Name: "Mock"} }
func (f *fakeGameEngine) Providers() []ports.ContentProvider { return nil }
func (f *fakeGameEngine) ResolveContent(ctx context.Context, inst domain.Instance) (domain.ContentSet, error) {
	return domain.ContentSet{}, nil
}
func (f *fakeGameEngine) ExportClientBundle(ctx context.Context, inst domain.Instance) (domain.Bundle, error) {
	return domain.Bundle{}, nil
}
func (f *fakeGameEngine) RuntimeSpec(inst domain.Instance) domain.RuntimeSpec {
	return domain.RuntimeSpec{}
}
func (f *fakeGameEngine) Telemetry(ctx context.Context) (domain.GameTelemetry, error) {
	return f.telemetry, nil
}
func (f *fakeGameEngine) AdmissionModel() domain.AdmissionModel { return "" }
func (f *fakeGameEngine) OperatorIDKind() domain.OperatorIDKind { return "" }

func TestTileSignalsWithGameEngines(t *testing.T) {
	valheim := &fakeGameEngine{
		telemetry: domain.GameTelemetry{
			State:        "Up",
			Online:       true,
			Players:      5,
			PlayersKnown: true,
			Uptime:       "3h 20m",
			CPU:          "250m",
			Memory:       "1500 Mi",
		},
	}
	minecraft := &fakeGameEngine{
		telemetry: domain.GameTelemetry{
			State:        "Up",
			Online:       true,
			Players:      2,
			PlayersKnown: true,
			Uptime:       "1d 2h",
			CPU:          "800m",
			Memory:       "6144 Mi",
			Loader:       "Fabric",
			PackName:     "Cobblemon Official",
		},
	}

	h := New(Config{
		ValheimGame:   valheim,
		MinecraftGame: minecraft,
	})

	sig := h.TileSignals(context.Background())

	if sig["state"] != "Up" || sig["online"] != true {
		t.Errorf("Valheim state/online unexpected: state=%v, online=%v", sig["state"], sig["online"])
	}
	if sig["players"] != 5 {
		t.Errorf("Valheim players = %v, want 5", sig["players"])
	}
	if sig["uptime"] != "3h 20m" || sig["cpu"] != "250m" || sig["mem"] != "1500 Mi" {
		t.Errorf("Valheim metrics unexpected: %v, %v, %v", sig["uptime"], sig["cpu"], sig["mem"])
	}

	if sig["mc_state"] != "Up" || sig["mc_players"] != 2 {
		t.Errorf("MC state/players unexpected: state=%v, players=%v", sig["mc_state"], sig["mc_players"])
	}
	if sig["mc_loader"] != "Fabric" || sig["mc_pack"] != "Cobblemon Official" {
		t.Errorf("MC loader/pack unexpected: loader=%v, pack=%v", sig["mc_loader"], sig["mc_pack"])
	}
	if sig["mc_uptime"] != "1d 2h" || sig["mc_cpu"] != "800m" || sig["mc_mem"] != "6144 Mi" {
		t.Errorf("MC metrics unexpected: %v, %v, %v", sig["mc_uptime"], sig["mc_cpu"], sig["mc_mem"])
	}
}

func TestDashboardPageWithInstances(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "dash.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mcRepo := store.NewInstanceRepo(st)
	_ = mcRepo.Upsert(domain.Instance{
		Number:    1,
		Slug:      "ducktopia",
		Name:      "Ducktopia",
		State:     domain.StateRunning,
		GameID:    domain.GameMinecraft,
		Loader:    domain.LoaderNeoForge,
		MCVersion: "1.21.1",
		Tier:      domain.TierMedium,
	})

	mcMgr := instances.NewInstanceManager(
		mcRepo, nil, nil, 32, 8, 4,
		"manifests/minecraft-modded", "192.168.20.225", nil, "minecraft-modded",
		instances.WithGameID(domain.GameMinecraft),
		instances.WithModsReader(func(ctx context.Context, num int) ([]string, error) {
			return []string{"jei"}, nil
		}),
	)

	vhRepo := store.NewValheimInstanceRepo(st)
	_ = vhRepo.Upsert(domain.Instance{
		Number:   1,
		Slug:     "midgard",
		Name:     "Midgard",
		State:    domain.StateRunning,
		GameID:   domain.GameValheim,
		Tier:     domain.TierMedium,
		Password: "pass",
	})

	vhMgr := instances.NewInstanceManager(
		vhRepo, nil, nil, 16, 4, 2,
		"manifests/valheim", "192.168.20.224", nil, "valheim",
		instances.WithGameID(domain.GameValheim),
		instances.WithModsReader(func(ctx context.Context, num int) ([]string, error) {
			return []string{"bepinex"}, nil
		}),
	)

	h := New(Config{
		GameNodeName:     "game-01",
		ValheimAddress:   "192.168.20.224:2456",
		MCInstances:      mcMgr,
		ValheimInstances: vhMgr,
		InstanceStats: func(ctx context.Context, insts []domain.Instance) map[int]InstanceStat {
			return map[int]InstanceStat{
				1: {Players: 3, PlayersKnown: true, Uptime: "5h"},
			}
		},
		ValheimInstanceStats: func(ctx context.Context, insts []domain.Instance) map[int]InstanceStat {
			return map[int]InstanceStat{
				1: {Players: 2, PlayersKnown: true, Uptime: "2h"},
			}
		},
	})

	app := fiber.New()
	h.Register(app)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / failed: %v, code: %d", err, resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "Ducktopia") || !strings.Contains(bodyStr, "Midgard") {
		t.Errorf("expected Ducktopia and Midgard on dashboard: %s", bodyStr)
	}
}

func TestDashboardSSEMain(t *testing.T) {
	// Test interval <= 0 fallback and initial push failure via CloseBodyStream
	h := New(Config{
		ModUpdateTotal: func() int { return 4 },
		SSEInterval:    0,
	})
	app := fiber.New()

	var fastCtx fasthttp.RequestCtx
	c := app.AcquireCtx(&fastCtx)
	defer app.ReleaseCtx(c)

	err := h.SSEMain(c)
	if err != nil {
		t.Fatal(err)
	}

	_ = fastCtx.Response.CloseBodyStream()
	time.Sleep(50 * time.Millisecond)
}

type mockModLister struct {
	mods []string
	err  error
}

func (m *mockModLister) GetInstalledMods(ctx context.Context, num int) ([]string, error) {
	return m.mods, m.err
}

func TestHasModsHelper(t *testing.T) {
	ctx := context.Background()
	if hasMods(ctx, nil, 1) {
		t.Errorf("expected false for nil modLister")
	}
	if hasMods(ctx, &mockModLister{err: fmt.Errorf("error")}, 1) {
		t.Errorf("expected false on error")
	}
	if hasMods(ctx, &mockModLister{mods: nil}, 1) {
		t.Errorf("expected false for empty mods")
	}
	if !hasMods(ctx, &mockModLister{mods: []string{"mod1"}}, 1) {
		t.Errorf("expected true for non-empty mods")
	}
}

type failAfterWriteOnce struct {
	writes int
}

func (f *failAfterWriteOnce) Write(p []byte) (int, error) {
	f.writes++
	if f.writes > 3 {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}

func TestDashboardRemainingEdges(t *testing.T) {
	app := fiber.New()

	// 1. Default Actor helper
	h := New(Config{})
	var fastCtx fasthttp.RequestCtx
	c := app.AcquireCtx(&fastCtx)
	defer app.ReleaseCtx(c)

	if a := h.cfg.Actor(c); a != "-" {
		t.Errorf("expected '-', got %q", a)
	}
	c.Locals("actor", "admin-user")
	if a := h.cfg.Actor(c); a != "admin-user" {
		t.Errorf("expected 'admin-user', got %q", a)
	}

	// 2. Valheim fallback to InstanceStats
	st, err := store.Open(filepath.Join(t.TempDir(), "dash2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	vhRepo := store.NewValheimInstanceRepo(st)
	_ = vhRepo.Upsert(domain.Instance{
		Number: 1, Name: "V1", State: domain.StateRunning, GameID: domain.GameValheim,
	})
	vhMgr := instances.NewInstanceManager(
		vhRepo, nil, nil, 16, 4, 2, "manifests/valheim", "192.168.20.224", nil, "valheim",
		instances.WithGameID(domain.GameValheim),
	)

	var statsCalled bool
	hStats := New(Config{
		ValheimInstances: vhMgr,
		InstanceStats: func(ctx context.Context, insts []domain.Instance) map[int]InstanceStat {
			statsCalled = true
			return map[int]InstanceStat{1: {Players: 1, PlayersKnown: true}}
		},
	})
	app.Get("/stats-fallback", hStats.DashboardPage)
	reqStats := httptest.NewRequest(http.MethodGet, "/stats-fallback", nil)
	respStats, err := app.Test(reqStats)
	if err != nil || respStats.StatusCode != http.StatusOK {
		t.Fatalf("unexpected response: %v, %d", err, respStats.StatusCode)
	}
	if !statsCalled {
		t.Errorf("expected InstanceStats called as fallback for Valheim")
	}

	// 3. SSEMain loop tick failure
	hSSE := New(Config{
		SSEInterval: 1 * time.Millisecond,
	})
	var sseCtx fasthttp.RequestCtx
	c2 := app.AcquireCtx(&sseCtx)
	defer app.ReleaseCtx(c2)

	if err := hSSE.SSEMain(c2); err != nil {
		t.Fatal(err)
	}
	_, _ = sseCtx.Response.WriteTo(&failAfterWriteOnce{})
}


