package valheim

import (
	"context"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	valheimmanifests "agrelha/internal/infra/manifests/valheim"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
)

type mockRuntime struct {
	status ports.Status
}

func (m *mockRuntime) Start(ctx context.Context, ref ports.ServerRef) error   { return nil }
func (m *mockRuntime) Stop(ctx context.Context, ref ports.ServerRef) error    { return nil }
func (m *mockRuntime) Restart(ctx context.Context, ref ports.ServerRef) error { return nil }
func (m *mockRuntime) Status(ctx context.Context, ref ports.ServerRef) (ports.Status, error) {
	return m.status, nil
}
func (m *mockRuntime) Metrics(ctx context.Context, ref ports.ServerRef) (ports.Metrics, error) {
	return ports.Metrics{}, nil
}
func (m *mockRuntime) Logs(ctx context.Context, ref ports.ServerRef, opts ports.LogOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockRuntime) WatchAvailability(ctx context.Context, ref ports.ServerRef, timeout time.Duration) error {
	return nil
}

func setupTestValheimHandler(t *testing.T) (*Handler, *store.Store, *instances.InstanceManager) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "valheim-test.db"))
	if err != nil {
		t.Fatal(err)
	}

	rt := &mockRuntime{
		status: ports.Status{Lifecycle: ports.LifecycleRunning, Available: true},
	}

	mgr := instances.NewInstanceManager(
		store.NewValheimInstanceRepo(st), nil, rt,
		16, 4, 2, "manifests/valheim", "192.168.20.224",
		valheimmanifests.New("ykhi.xyz/gameserver=true", "valheim"),
		"valheim",
		instances.WithGameID(domain.GameValheim),
		instances.WithBackupsDir(t.TempDir()),
	)

	h := New(Config{
		ValheimInstances: mgr,
	})

	return h, st, mgr
}

func TestValheimDashboard(t *testing.T) {
	h, st, mgr := setupTestValheimHandler(t)
	defer st.Close()

	ctx := context.Background()
	_, err := mgr.CreateInstance(ctx, domain.Instance{
		GameID: domain.GameValheim,
		Name:   "Odin's World",
		Tier:   domain.TierMedium,
	}, "", "tester")
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	app := fiber.New()
	h.Register(app)

	req := httptest.NewRequest("GET", "/valheim", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("GET /valheim failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
}

func TestValheimLifecycleEndpoints(t *testing.T) {
	h, st, mgr := setupTestValheimHandler(t)
	defer st.Close()

	ctx := context.Background()
	inst, err := mgr.CreateInstance(ctx, domain.Instance{
		GameID: domain.GameValheim,
		Name:   "Viking Realm",
		Tier:   domain.TierSmall,
	}, "", "tester")
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	app := fiber.New()
	h.Register(app)

	// Test Start
	req := httptest.NewRequest("POST", "/api/valheim/instances/1/start", nil)
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("start instance failed: %v, status: %d", err, resp.StatusCode)
	}

	// Test Restart
	req = httptest.NewRequest("POST", "/api/valheim/instances/1/restart", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("restart instance failed: %v, status: %d", err, resp.StatusCode)
	}

	// Test Stop
	req = httptest.NewRequest("POST", "/api/valheim/instances/1/stop", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("stop instance failed: %v, status: %d", err, resp.StatusCode)
	}

	_ = inst
}

func TestValheimInstancePage(t *testing.T) {
	h, st, mgr := setupTestValheimHandler(t)
	defer st.Close()

	ctx := context.Background()
	_, err := mgr.CreateInstance(ctx, domain.Instance{
		GameID:   domain.GameValheim,
		Name:     "Midgard",
		Password: "secretpassword",
		Tier:     domain.TierLarge,
	}, "denikson-BepInExPack_Valheim\n", "tester")
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	app := fiber.New()
	h.Register(app)

	for _, tab := range []string{"overview", "mods", "configs", "settings"} {
		req := httptest.NewRequest("GET", "/valheim/1/"+tab, nil)
		resp, err := app.Test(req)
		if err != nil || resp.StatusCode != fiber.StatusOK {
			t.Fatalf("GET /valheim/1/%s failed: %v, code: %d", tab, err, resp.StatusCode)
		}
	}
}

func TestValheimWizardCreate(t *testing.T) {
	h, st, _ := setupTestValheimHandler(t)
	defer st.Close()

	app := fiber.New()
	h.Register(app)

	body := `{"name":"Asgard","password":"valheimpass","seed":"odinseed","tier":"medium","source":"scratch","raw_mods":"ValheimModding-Jotunn"}`
	req := httptest.NewRequest("POST", "/api/valheim/wizard/create", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("wizard create failed: %v, status: %d", err, resp.StatusCode)
	}
}

func TestValheimLegacyRedirects(t *testing.T) {
	h, st, mgr := setupTestValheimHandler(t)
	defer st.Close()

	ctx := context.Background()
	_, _ = mgr.CreateInstance(ctx, domain.Instance{
		GameID: domain.GameValheim,
		Name:   "Valheim Default",
	}, "", "tester")

	app := fiber.New()
	h.Register(app)

	req := httptest.NewRequest("GET", "/mods", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusTemporaryRedirect {
		t.Errorf("expected 307 redirect for /mods, got %d", resp.StatusCode)
	}

	req = httptest.NewRequest("GET", "/configs", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusTemporaryRedirect {
		t.Errorf("expected 307 redirect for /configs, got %d", resp.StatusCode)
	}
}
