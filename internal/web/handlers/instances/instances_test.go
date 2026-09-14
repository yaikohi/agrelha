package instances

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"agrelha/internal/app/instances"
	"agrelha/internal/app/modupdates"
	"agrelha/internal/domain"
	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/manifests"
	k8sruntime "agrelha/internal/infra/runtime/k8s"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
)

func setupTestInstancesHandler(t *testing.T) (*Handler, *store.Store, *k8s.Client, *instances.InstanceManager) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}

	cs := fake.NewSimpleClientset(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-ducktopia-01-mods", Namespace: "minecraft-modded"},
			Data: map[string]string{
				"MINECRAFT_VERSION": "1.21.1",
				"NEOFORGE_VERSION":  "recommended",
				"mods.txt":          "jei\nferrite-core\n",
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-ducktopia-01-configs", Namespace: "minecraft-modded"},
			Data: map[string]string{
				"server.properties": "difficulty=normal\n",
			},
		},
	)

	mck8s := k8s.NewWithClientset(cs, "minecraft-modded", "minecraft-modded")
	var mgr *instances.InstanceManager
	mgr = instances.NewInstanceManager(
		store.NewInstanceRepo(st), nil, k8sruntime.New(mck8s), 24, 4, 2, "manifests/minecraft-modded", "192.168.20.224", manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded"), "minecraft-modded",
		instances.WithBackupsDir(t.TempDir()),
		instances.WithConfigsReader(func(ctx context.Context, num int) (map[string]string, error) {
			inst, err := mgr.GetInstance(ctx, num)
			if err != nil {
				return nil, err
			}
			return mck8s.ConfigMapData(ctx, inst.ConfigsCMName())
		}),
		instances.WithModsReader(func(ctx context.Context, num int) ([]string, error) {
			inst, err := mgr.GetInstance(ctx, num)
			if err != nil {
				return nil, err
			}
			cm, err := mck8s.ConfigMapData(ctx, inst.ModsCMName())
			if err != nil {
				return nil, err
			}
			return strings.Split(strings.TrimSpace(cm["mods.txt"]), "\n"), nil
		}),
	)

	h := New(Config{
		MCInstances: mgr,
	})

	return h, st, mck8s, mgr
}

func TestMCDashboardEndpoint(t *testing.T) {
	h, st, _, mgr := setupTestInstancesHandler(t)
	defer st.Close()

	// Seed instance
	_, err := mgr.CreateInstance(context.Background(), domain.Instance{
		Name:      "Ducktopia",
		MCVersion: "1.21.1",
		Loader:    domain.LoaderNeoForge,
		Tier:      domain.TierMedium,
		State:     domain.StateStopped,
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	h.Register(app)

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/minecraft", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestMCInstanceLifecycleEndpoints(t *testing.T) {
	h, st, _, mgr := setupTestInstancesHandler(t)
	defer st.Close()

	inst, err := mgr.CreateInstance(context.Background(), domain.Instance{
		Name:      "Ducktopia",
		MCVersion: "1.21.1",
		Loader:    domain.LoaderNeoForge,
		Tier:      domain.TierMedium,
		State:     domain.StateStopped,
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	h.Register(app)

	// Start
	req := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/instances/1/start", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("start status = %d, want 200", resp.StatusCode)
	}

	// Stop
	req = httptest.NewRequest(fiber.MethodPost, "/api/minecraft/instances/1/stop", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("stop status = %d, want 200", resp.StatusCode)
	}

	// Delete
	req = httptest.NewRequest(fiber.MethodDelete, "/api/minecraft/instances/1", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("delete status = %d, want 200", resp.StatusCode)
	}

	_ = inst
}

func TestInstanceStatsCaching(t *testing.T) {
	h, st, _, _ := setupTestInstancesHandler(t)
	defer st.Close()

	insts := []domain.Instance{
		{Number: 1, Name: "Test", State: domain.StateRunning},
	}

	stats := h.InstanceStats(context.Background(), insts)
	if _, ok := stats[1]; !ok {
		t.Fatalf("expected stats for instance 1")
	}

	// Immediate next call should use cache
	stats2 := h.InstanceStats(context.Background(), insts)
	if _, ok := stats2[1]; !ok {
		t.Fatalf("expected stats for instance 1 from cache")
	}
}

func TestConfigFilenameRegex(t *testing.T) {
	valid := []string{"config.toml", "server.properties", "sub/dir/config.json", "custom.yaml", "settings.cfg"}
	for _, f := range valid {
		if !mcCfgNameRe.MatchString(f) {
			t.Errorf("expected %q to be valid config name", f)
		}
	}

	invalid := []string{"foo.exe", ".hidden", "config.sh", "bad..ext"}
	for _, f := range invalid {
		if mcCfgNameRe.MatchString(f) {
			t.Errorf("expected %q to be invalid config name", f)
		}
	}
}

type fakeMCGame struct {
	bundle domain.Bundle
}

func (f *fakeMCGame) ID() domain.GameID                  { return domain.GameMinecraft }
func (f *fakeMCGame) Display() domain.Display            { return domain.Display{Name: "Minecraft"} }
func (f *fakeMCGame) Providers() []ports.ContentProvider { return nil }
func (f *fakeMCGame) ResolveContent(ctx context.Context, inst domain.Instance) (domain.ContentSet, error) {
	return domain.ContentSet{}, nil
}
func (f *fakeMCGame) ExportClientBundle(ctx context.Context, inst domain.Instance) (domain.Bundle, error) {
	return f.bundle, nil
}
func (f *fakeMCGame) RuntimeSpec(inst domain.Instance) domain.RuntimeSpec {
	return domain.RuntimeSpec{}
}
func (f *fakeMCGame) Telemetry(ctx context.Context) (domain.GameTelemetry, error) {
	return domain.GameTelemetry{}, nil
}
func (f *fakeMCGame) AdmissionModel() domain.AdmissionModel { return domain.AdmissionAllowlist }
func (f *fakeMCGame) OperatorIDKind() domain.OperatorIDKind { return domain.IDKindUsername }

func TestMCInstanceExportWithGameEngine(t *testing.T) {
	h, st, _, mgr := setupTestInstancesHandler(t)
	defer st.Close()

	// Seed instance 1
	repo := store.NewInstanceRepo(st)
	_ = repo.Upsert(domain.Instance{
		Number: 1, Name: "Ducktopia", Slug: "mc-ducktopia-01",
	})

	h.cfg.MinecraftGame = &fakeMCGame{
		bundle: domain.Bundle{
			Filename:    "ducktopia-1.21.1.mrpack",
			ContentType: "application/x-modrinth-modpack+zip",
			Data:        []byte("mock-mrpack-archive"),
		},
	}

	app := fiber.New()
	app.Get("/instances/:num/export", h.MCInstanceExport)

	req := httptest.NewRequest("GET", "/instances/1/export", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("GET /instances/1/export: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "mock-mrpack-archive" {
		t.Errorf("body = %s, want mock-mrpack-archive", string(body))
	}
	if disp := resp.Header.Get("Content-Disposition"); !strings.Contains(disp, "ducktopia-1.21.1.mrpack") {
		t.Errorf("Content-Disposition = %s, want ducktopia-1.21.1.mrpack", disp)
	}
	_ = mgr
}

type mockModUpdatesCatalog struct {
	versions map[string]string
}

func (m *mockModUpdatesCatalog) LatestVersion(ctx context.Context, ns, name string) (string, []string, error) {
	return "", nil, nil
}
func (m *mockModUpdatesCatalog) ResolveTree(ctx context.Context, ns, name string) ([]string, error) {
	return nil, nil
}
func (m *mockModUpdatesCatalog) LatestVersionForInstance(ctx context.Context, ref domain.ModRef, inst domain.Instance) (string, []string, error) {
	key := fmt.Sprintf("%s|%s", ref.Name, inst.Loader)
	return m.versions[key], nil, nil
}
func (m *mockModUpdatesCatalog) ResolveTreeForInstance(ctx context.Context, ref domain.ModRef, inst domain.Instance) ([]string, error) {
	return nil, nil
}

func TestMCModUpdatesEndpoints(t *testing.T) {
	h, st, _, mgr := setupTestInstancesHandler(t)
	defer st.Close()

	repo := store.NewInstanceRepo(st)
	_ = repo.Upsert(domain.Instance{
		Number: 1, Name: "Ducktopia", Slug: "ducktopia",
		GameID: domain.GameMinecraft, Loader: domain.LoaderFabric, MCVersion: "1.21.1", Source: domain.SourceModlist,
	})

	cat := &mockModUpdatesCatalog{
		versions: map[string]string{
			"jei|fabric": "15.0.0",
		},
	}
	h.cfg.ModUpdates = modupdates.New(mgr, cat, modupdates.WithGameID(domain.GameMinecraft), modupdates.WithRestorePoints(st))

	app := fiber.New()
	h.RegisterProtected(app)

	// 1. Check updates
	req := httptest.NewRequest("POST", "/api/minecraft/1/mods/updates/check", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("check status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "mod-updates-panel") {
		t.Errorf("expected mod-updates-panel in response: %s", string(body))
	}
}
