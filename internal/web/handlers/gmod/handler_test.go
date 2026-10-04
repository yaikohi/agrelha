package gmod

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	gmodmanifests "agrelha/internal/infra/manifests/gmod"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
)

type mockRuntime struct{}

func (m *mockRuntime) Start(context.Context, ports.ServerRef) error   { return nil }
func (m *mockRuntime) Stop(context.Context, ports.ServerRef) error    { return nil }
func (m *mockRuntime) Restart(context.Context, ports.ServerRef) error { return nil }
func (m *mockRuntime) Status(context.Context, ports.ServerRef) (ports.Status, error) {
	return ports.Status{Available: true, Lifecycle: ports.LifecycleRunning}, nil
}
func (m *mockRuntime) Metrics(context.Context, ports.ServerRef) (ports.Metrics, error) {
	return ports.Metrics{Known: true, CPUMillicores: 100, MemoryMiB: 1024}, nil
}
func (m *mockRuntime) Logs(context.Context, ports.ServerRef, ports.LogOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockRuntime) WatchAvailability(context.Context, ports.ServerRef, time.Duration) error {
	return nil
}

type mockWorkshop struct {
	col domain.WorkshopCollection
	err error
}

func (m *mockWorkshop) GetCollection(ctx context.Context, id string) (domain.WorkshopCollection, error) {
	if m.err != nil {
		return domain.WorkshopCollection{}, m.err
	}
	return m.col, nil
}

func setupTestApp(t *testing.T) (*fiber.App, *Handler, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	repo := store.NewGameInstanceRepo(st, domain.GModProfile)
	mgr := instances.NewInstanceManager(
		repo,
		nil,
		&mockRuntime{},
		24, 4, 2,
		"manifests/gmod",
		"192.168.20.226",
		gmodmanifests.New("", "gmod"),
		"gmod",
		instances.WithGameID(domain.GameGMod),
	)

	h := New(Config{
		Instances: mgr,
		Workshop: &mockWorkshop{
			col: domain.WorkshopCollection{
				ID:          "104604903",
				Title:       "TTT Fun Pack",
				ItemCount:   12,
				TimeUpdated: time.Now().Add(1 * time.Hour),
			},
		},
	})

	app := fiber.New()
	h.RegisterProtected(app)
	return app, h, st
}

func TestGModDashboardAndWizardFlow(t *testing.T) {
	app, _, st := setupTestApp(t)
	defer st.Close()

	req := httptest.NewRequest("GET", "/gmod", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("GET /gmod failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /gmod status = %d, want 200", resp.StatusCode)
	}

	req = httptest.NewRequest("GET", "/gmod/create", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("GET /gmod/create failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /gmod/create status = %d, want 200", resp.StatusCode)
	}

	createPayload := `{"name":"Trouble in Terrorist Town","gamemode":"terrortown","map":"ttt_minecraft_b5","collectionID":"104604903","password":"secret","tier":"small"}`
	createReq := httptest.NewRequest("POST", "/api/gmod/wizard/create", strings.NewReader(createPayload))
	createReq.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(createReq)
	if err != nil {
		t.Fatalf("POST /api/gmod/wizard/create failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST /api/gmod/wizard/create status = %d, want 200", resp.StatusCode)
	}

	req = httptest.NewRequest("GET", "/gmod/1/overview", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("GET /gmod/1/overview failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /gmod/1/overview status = %d, want 200", resp.StatusCode)
	}

	req = httptest.NewRequest("GET", "/gmod/1", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("GET /gmod/1 failed: %v", err)
	}
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusTemporaryRedirect {
		t.Errorf("GET /gmod/1 status = %d, want redirect", resp.StatusCode)
	}

	lifecycleEndpoints := []struct {
		method string
		path   string
	}{
		{"POST", "/api/gmod/instances/1/start"},
		{"POST", "/api/gmod/instances/1/stop"},
		{"POST", "/api/gmod/instances/1/restart"},
		{"DELETE", "/api/gmod/instances/1"},
	}

	for _, ep := range lifecycleEndpoints {
		req = httptest.NewRequest(ep.method, ep.path, nil)
		resp, err = app.Test(req)
		if err != nil {
			t.Fatalf("%s %s failed: %v", ep.method, ep.path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s %s status = %d, want 200", ep.method, ep.path, resp.StatusCode)
		}
	}
}
