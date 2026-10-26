package dashboard

import (
	"agrelha/internal/web/shared"
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

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

func TestDashboardPage(t *testing.T) {
	h := New(Config{
		GameNodeName:   "game-01",
		ValheimAddress: "192.168.20.224:2456",
	})

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		if c.Query("admin") == "1" {
			c.Locals(shared.PrincipalKey, domain.NewPrincipal(domain.Identity{
				Subject: "admin", Roles: []domain.Role{domain.RoleAdmin},
			}))
		}
		return c.Next()
	})
	h.Register(app)

	// 1. The dashboard stays public: a guest still gets a page.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", resp.StatusCode)
	}
	guestBody, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(guestBody), "game-01") {
		t.Errorf("guest dashboard must not expose the node name")
	}

	// 2. An admin sees the operational detail.
	reqAdmin := httptest.NewRequest(http.MethodGet, "/?admin=1", nil)
	respAdmin, err := app.Test(reqAdmin)
	if err != nil {
		t.Fatal(err)
	}
	adminBody, _ := io.ReadAll(respAdmin.Body)
	if !strings.Contains(string(adminBody), "game-01") {
		t.Errorf("expected node name game-01 in the admin dashboard body")
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

func (f *fakeGameEngine) ExportClientBundle(ctx context.Context, inst domain.Instance) (domain.Bundle, error) {
	return domain.Bundle{}, nil
}
func (f *fakeGameEngine) RuntimeSpec(inst domain.Instance) domain.RuntimeSpec {
	return domain.RuntimeSpec{}
}
func (f *fakeGameEngine) Telemetry(ctx context.Context) (domain.GameTelemetry, error) {
	return f.telemetry, nil
}

func TestTileSignalsWithGameEngines(t *testing.T) {
	valheim := &fakeGameEngine{
		telemetry: domain.GameTelemetry{
			Lifecycle:    "running",
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
			Lifecycle:    "running",
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

	if sig["lifecycle"] != "running" || sig["online"] != true {
		t.Errorf("Valheim lifecycle/online unexpected: lifecycle=%v, online=%v", sig["lifecycle"], sig["online"])
	}
	if sig["players"] != 5 {
		t.Errorf("Valheim players = %v, want 5", sig["players"])
	}
	if sig["uptime"] != "3h 20m" || sig["cpu"] != "250m" || sig["mem"] != "1500 Mi" {
		t.Errorf("Valheim metrics unexpected: %v, %v, %v", sig["uptime"], sig["cpu"], sig["mem"])
	}

	if sig["mc_lifecycle"] != "running" || sig["mc_players"] != 2 {
		t.Errorf("MC lifecycle/players unexpected: lifecycle=%v, players=%v", sig["mc_lifecycle"], sig["mc_players"])
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
		Number: 1,
		Slug:   "ducktopia",
		Name:   "Ducktopia",
		State:  domain.StateRunning,
		GameID: domain.GameMinecraft,
		Tier:   domain.TierMedium,
		Minecraft: &domain.MinecraftConfig{
			Loader:    domain.LoaderNeoForge,
			MCVersion: "1.21.1",
		},
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
		Number:  1,
		Slug:    "midgard",
		Name:    "Midgard",
		State:   domain.StateRunning,
		GameID:  domain.GameValheim,
		Tier:    domain.TierMedium,
		Valheim: &domain.ValheimConfig{Password: "pass"},
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

func TestTileSignalsWithPerInstanceStreams(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "dash_signals.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mcRepo := store.NewInstanceRepo(st)
	_ = mcRepo.Upsert(domain.Instance{
		Number: 1,
		Slug:   "ducktopia",
		Name:   "Ducktopia",
		State:  domain.StateRunning,
		GameID: domain.GameMinecraft,
		Tier:   domain.TierMedium,
		Minecraft: &domain.MinecraftConfig{
			Loader:    domain.LoaderNeoForge,
			MCVersion: "1.21.1",
		},
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
		Number:  1,
		Slug:    "midgard",
		Name:    "Midgard",
		State:   domain.StateRunning,
		GameID:  domain.GameValheim,
		Tier:    domain.TierMedium,
		Valheim: &domain.ValheimConfig{Password: "pass"},
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

	sig := h.TileSignals(context.Background())

	if sig["inst_minecraft_1_online"] != true {
		t.Fatalf("expected inst_minecraft_1_online to be true; at 2am during an incident an operator will assume Minecraft is offline when it is actually running")
	}
	if sig["inst_minecraft_1_state"] != "Running" {
		t.Fatalf("expected inst_minecraft_1_state to be 'Running', got %v; at 2am operators see stale status on the dashboard", sig["inst_minecraft_1_state"])
	}
	if sig["inst_minecraft_1_lamp"] != "lamp lamp-lit" {
		t.Fatalf("expected inst_minecraft_1_lamp to be 'lamp lamp-lit', got %v; at 2am the server health lamp will misreport server reachability", sig["inst_minecraft_1_lamp"])
	}
	if sig["inst_minecraft_1_players"] != 3 {
		t.Fatalf("expected inst_minecraft_1_players to be 3, got %v; at 2am operators cannot tell if players are affected by maintenance", sig["inst_minecraft_1_players"])
	}
	if sig["inst_minecraft_1_presence"] != "3 playing, up 5h" {
		t.Fatalf("expected inst_minecraft_1_presence to be '3 playing, up 5h', got %v; at 2am uptime and player count are missing from live dashboard tiles", sig["inst_minecraft_1_presence"])
	}
	mcMods, ok := sig["inst_minecraft_1_mods"].(map[string]bool)
	if !ok || !mcMods["jei"] {
		t.Fatalf("expected inst_minecraft_1_mods to contain 'jei', got %v; at 2am mod status checks fail to reflect live installed mods without page reload", sig["inst_minecraft_1_mods"])
	}
	if sig["inst_minecraft_1_mods_count"] != 1 {
		t.Fatalf("expected inst_minecraft_1_mods_count to be 1, got %v; at 2am mod counts show 0 on live tiles", sig["inst_minecraft_1_mods_count"])
	}

	if sig["inst_valheim_1_online"] != true {
		t.Fatalf("expected inst_valheim_1_online to be true; at 2am during a restart loop Valheim appears offline when it is running")
	}
	if sig["inst_valheim_1_presence"] != "2 playing, up 2h" {
		t.Fatalf("expected inst_valheim_1_presence to be '2 playing, up 2h', got %v; at 2am Valheim presence fails to stream", sig["inst_valheim_1_presence"])
	}
	vhMods, ok := sig["inst_valheim_1_mods"].(map[string]bool)
	if !ok || !vhMods["bepinex"] {
		t.Fatalf("expected inst_valheim_1_mods to contain 'bepinex', got %v; at 2am Valheim mod catalog reports stale state", sig["inst_valheim_1_mods"])
	}
}

func TestDashboardRendersSavedInstancesForValheimAndGMod(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "dash_saved.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	vhRepo := store.NewValheimInstanceRepo(st)
	_ = vhRepo.Upsert(domain.Instance{
		Number:  1,
		Slug:    "viking-haven",
		Name:    "Viking Haven",
		State:   domain.StateStopped,
		GameID:  domain.GameValheim,
		Tier:    domain.TierMedium,
		Valheim: &domain.ValheimConfig{Password: "secret"},
	})
	vhMgr := instances.NewInstanceManager(
		vhRepo, nil, nil, 16, 4, 2,
		"manifests/valheim", "192.168.20.224", nil, "valheim",
		instances.WithGameID(domain.GameValheim),
	)

	gmodRepo := store.NewGameInstanceRepo(st, domain.GModProfile)
	_ = gmodRepo.Upsert(domain.Instance{
		Number: 1,
		Slug:   "prop-hunt",
		Name:   "Prop Hunt",
		State:  domain.StateStopped,
		GameID: domain.GameGMod,
		Tier:   domain.TierMedium,
		GMod:   &domain.GModConfig{Gamemode: "prop_hunt", Map: "ph_office"},
	})
	gmodMgr := instances.NewInstanceManager(
		gmodRepo, nil, nil, 16, 4, 2,
		"manifests/gmod", "192.168.20.226", nil, "gmod",
		instances.WithGameID(domain.GameGMod),
	)

	h := New(Config{
		GameNodeName:   "game-01",
		ValheimAddress: "192.168.20.224:2456",
		InstanceManagers: map[domain.GameID]*instances.InstanceManager{
			domain.GameValheim: vhMgr,
			domain.GameGMod:    gmodMgr,
		},
	})

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(shared.PrincipalKey, domain.NewPrincipal(domain.Identity{
			Subject: "admin", Roles: []domain.Role{domain.RoleAdmin},
		}))
		return c.Next()
	})
	h.Register(app)

	req := httptest.NewRequest(http.MethodGet, "/?admin=1", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !strings.Contains(html, "Viking Haven") {
		t.Fatalf("Dashboard did not render saved Valheim instance 'Viking Haven': body was %s. At 2am an admin sees 'Nothing running' and assumes their saved Valheim worlds were deleted.", html)
	}
	if !strings.Contains(html, "Prop Hunt") {
		t.Fatalf("Dashboard did not render saved GMod instance 'Prop Hunt': body was %s. At 2am an admin sees 'Nothing running' and assumes their saved GMod worlds were deleted.", html)
	}
}
