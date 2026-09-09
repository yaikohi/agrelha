package instances

import (
	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"agrelha/internal/infra/manifests"
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"agrelha/internal/infra/kube"
	k8sruntime "agrelha/internal/infra/runtime/k8s"
	"agrelha/internal/infra/store"
	"agrelha/internal/platform/config"
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
	mgr := instances.NewInstanceManager(
		store.NewInstanceRepo(st), nil, k8sruntime.New(mck8s), 24, 4, 2, "manifests/minecraft-modded", "192.168.20.224", manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded"), "minecraft-modded")

	h := New(Config{
		Cfg:         &config.Config{BackupsDir: t.TempDir()},
		Store:       st,
		MCK8s:       mck8s,
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
