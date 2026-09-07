package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"agrelha/internal/config"
	"agrelha/internal/k8s"
	"agrelha/internal/minecraft"
	"agrelha/internal/store"
)

func setupTestMCServer(t *testing.T) (*FiberServer, *store.Store, *minecraft.InstanceManager) {
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
	mgr := minecraft.NewInstanceManager(st, nil, mck8s, 24, 4, 2, "manifests/minecraft-modded")

	s := &FiberServer{
		App:         fiber.New(),
		cfg:         &config.Config{},
		store:       st,
		mck8s:       mck8s,
		mcInstances: mgr,
		mcRconPool:  minecraft.NewRconPool("testpass", 3*time.Second),
	}
	s.RegisterFiberRoutes()
	return s, st, mgr
}

func TestMCDashboardEmpty(t *testing.T) {
	s, st, _ := setupTestMCServer(t)
	defer st.Close()

	req := httptest.NewRequest(fiber.MethodGet, "/minecraft", nil)
	resp, err := s.App.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	for _, want := range []string{"Minecraft Instances", "24 GiB", "Create your first world"} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}

func TestMCWizardPage(t *testing.T) {
	s, st, _ := setupTestMCServer(t)
	defer st.Close()

	req := httptest.NewRequest(fiber.MethodGet, "/minecraft/create", nil)
	resp, err := s.App.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	for _, want := range []string{"Create Minecraft World", "Step 1: Name Your World"} {
		if !strings.Contains(html, want) {
			t.Errorf("wizard missing %q", want)
		}
	}
}

func TestMCInstanceCreateAndDetail(t *testing.T) {
	s, st, mgr := setupTestMCServer(t)
	defer st.Close()

	// 1. Create instance via wizard endpoint
	payload := map[string]any{
		"name":       "Ducktopia",
		"source":     "scratch",
		"loader":     "neoforge",
		"mc_version": "1.21.1",
		"tier":       "small",
		"gamemode":   "survival",
		"difficulty": "normal",
		"world_type": "default",
	}
	payloadBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/create", bytes.NewReader(payloadBytes))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.App.Test(req)
	if err != nil {
		t.Fatalf("wizard create request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// 2. Verify stored instance
	inst, err := mgr.GetInstance(context.Background(), 1)
	if err != nil {
		t.Fatalf("get instance 1 failed: %v", err)
	}
	if inst == nil {
		t.Fatalf("expected instance 1, got nil")
	}
	if inst.Name != "Ducktopia" || inst.Number != 1 || inst.LBIP != "192.168.20.225" {
		t.Fatalf("unexpected instance: %+v", inst)
	}

	// 3. Test Detail Overview
	req = httptest.NewRequest(fiber.MethodGet, "/minecraft/1/overview", nil)
	resp, err = s.App.Test(req)
	if err != nil {
		t.Fatalf("overview request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("overview status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "192.168.20.225:25565") {
		t.Errorf("overview missing connection string")
	}

	// 4. Test Detail Mods Tab
	req = httptest.NewRequest(fiber.MethodGet, "/minecraft/1/mods", nil)
	resp, err = s.App.Test(req)
	if err != nil {
		t.Fatalf("mods tab request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("mods tab status = %d, want 200", resp.StatusCode)
	}

	// 5. Test Detail Console Tab
	req = httptest.NewRequest(fiber.MethodGet, "/minecraft/1/console", nil)
	resp, err = s.App.Test(req)
	if err != nil {
		t.Fatalf("console tab request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("console tab status = %d, want 200", resp.StatusCode)
	}

	// 6. Test Detail Backups Tab
	req = httptest.NewRequest(fiber.MethodGet, "/minecraft/1/backups", nil)
	resp, err = s.App.Test(req)
	if err != nil {
		t.Fatalf("backups tab request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("backups tab status = %d, want 200", resp.StatusCode)
	}

	// 7. Test Detail Settings Tab
	req = httptest.NewRequest(fiber.MethodGet, "/minecraft/1/settings", nil)
	resp, err = s.App.Test(req)
	if err != nil {
		t.Fatalf("settings tab request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("settings tab status = %d, want 200", resp.StatusCode)
	}

	// 8. Update settings
	form := url.Values{
		"name":       {"Ducktopia Remastered"},
		"motd":       {"Welcome to Ducktopia"},
		"tier":       {"medium"},
		"mc_version": {"1.21.1"},
	}
	req = httptest.NewRequest(fiber.MethodPost, "/api/minecraft/1/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err = s.App.Test(req)
	if err != nil {
		t.Fatalf("settings save request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("settings save status = %d, want 200", resp.StatusCode)
	}

	updated, err := mgr.GetInstance(context.Background(), 1)
	if err != nil {
		t.Fatalf("get updated instance failed: %v", err)
	}
	if updated.Name != "Ducktopia Remastered" || updated.Tier != minecraft.TierMedium {
		t.Fatalf("expected updated settings, got: %+v", updated)
	}

	// 9. Lifecycle: Stop & Start
	req = httptest.NewRequest(fiber.MethodPost, "/api/minecraft/instances/1/stop", nil)
	resp, err = s.App.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("stop instance failed: %v, status: %d", err, resp.StatusCode)
	}

	req = httptest.NewRequest(fiber.MethodPost, "/api/minecraft/instances/1/start", nil)
	resp, err = s.App.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("start instance failed: %v, status: %d", err, resp.StatusCode)
	}

	// 10. Delete instance
	req = httptest.NewRequest(fiber.MethodDelete, "/api/minecraft/instances/1", nil)
	resp, err = s.App.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("delete instance failed: %v, status: %d", err, resp.StatusCode)
	}

	instances, err := mgr.ListInstances(context.Background())
	if err != nil {
		t.Fatalf("list instances failed: %v", err)
	}
	if len(instances) != 0 {
		t.Fatalf("expected 0 instances after delete, got %d", len(instances))
	}
}
