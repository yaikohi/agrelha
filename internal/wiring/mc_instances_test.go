package wiring_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
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

	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/manifests"
	"agrelha/internal/infra/rcon"
	k8sruntime "agrelha/internal/infra/runtime/k8s"
	"agrelha/internal/infra/store"
	"agrelha/internal/platform/config"
	"agrelha/internal/wiring"
)

func setupTestMCServer(t *testing.T) (*fiber.App, *store.Store, *instances.InstanceManager, wiring.Deps, *config.Config) {
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
	mgr := instances.NewInstanceManager(store.NewInstanceRepo(st), nil, k8sruntime.New(mck8s), 24, 4, 2, "manifests/minecraft-modded", "192.168.20.224", manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded"), "minecraft-modded")

	cfg := &config.Config{
		MinecraftNamespace:  "minecraft-modded",
		MinecraftDeployment: "minecraft-modded",
	}

	d := wiring.Deps{
		Store:       st,
		MCK8s:       mck8s,
		MCInstances: mgr,
		MCRconPool:  rcon.NewPool("testpass", 3*time.Second),
	}
	app := wiring.BuildServer(context.Background(), cfg, d)
	return app, st, mgr, d, cfg
}

func TestMCDashboardEmpty(t *testing.T) {
	app, st, _, _, _ := setupTestMCServer(t)
	defer st.Close()

	req := httptest.NewRequest(fiber.MethodGet, "/minecraft", nil)
	resp, err := app.Test(req)
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
	app, st, _, _, _ := setupTestMCServer(t)
	defer st.Close()

	req := httptest.NewRequest(fiber.MethodGet, "/minecraft/create", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	rawHTML := string(body)
	htmlContent := html.UnescapeString(rawHTML)

	for _, want := range []string{"Create Minecraft World", "Step 1: Name Your World"} {
		if !strings.Contains(htmlContent, want) {
			t.Errorf("wizard missing %q", want)
		}
	}
	if strings.Contains(rawHTML, `data-on:submit.prevent`) {
		t.Errorf("wizard contains unsupported data-on:submit.prevent, which causes native page reloads")
	}
	if !strings.Contains(htmlContent, `@post('/api/minecraft/wizard/modpacks/search')`) {
		t.Errorf("wizard missing modpack search @post handler")
	}
	if !strings.Contains(htmlContent, `@post('/api/minecraft/wizard/mods/search')`) {
		t.Errorf("wizard missing mods search @post handler")
	}
	if !strings.Contains(htmlContent, `@post('/api/minecraft/wizard/create')`) {
		t.Errorf("wizard missing create @post handler")
	}
	if strings.Contains(htmlContent, `$loader = $bestLoader`) {
		t.Errorf("wizard must not auto-override user loader choice with $bestLoader")
	}
	if !strings.Contains(htmlContent, `$loader = 'neoforge'`) || !strings.Contains(htmlContent, `$loader = 'fabric'`) {
		t.Errorf("wizard must contain explicit NeoForge and Fabric loader selection buttons")
	}
}

func TestMCInstanceCreateAndDetail(t *testing.T) {
	app, st, mgr, _, _ := setupTestMCServer(t)
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

	resp, err := app.Test(req)
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
	resp, err = app.Test(req)
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
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("mods tab request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("mods tab status = %d, want 200", resp.StatusCode)
	}

	// 5. Test Detail Console Tab
	req = httptest.NewRequest(fiber.MethodGet, "/minecraft/1/console", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("console tab request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("console tab status = %d, want 200", resp.StatusCode)
	}

	// 6. Test Detail Backups Tab
	req = httptest.NewRequest(fiber.MethodGet, "/minecraft/1/backups", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("backups tab request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("backups tab status = %d, want 200", resp.StatusCode)
	}

	// 7. Test Detail Settings Tab
	req = httptest.NewRequest(fiber.MethodGet, "/minecraft/1/settings", nil)
	resp, err = app.Test(req)
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
	resp, err = app.Test(req)
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
	if updated.Name != "Ducktopia Remastered" || updated.Tier != domain.TierMedium {
		t.Fatalf("expected updated settings, got: %+v", updated)
	}

	// 9. Lifecycle: Stop & Start
	req = httptest.NewRequest(fiber.MethodPost, "/api/minecraft/instances/1/stop", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("stop instance failed: %v, status: %d", err, resp.StatusCode)
	}

	req = httptest.NewRequest(fiber.MethodPost, "/api/minecraft/instances/1/start", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("start instance failed: %v, status: %d", err, resp.StatusCode)
	}

	// 10. Delete instance
	req = httptest.NewRequest(fiber.MethodDelete, "/api/minecraft/instances/1", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("delete instance failed: %v, status: %d", err, resp.StatusCode)
	}

	instList, err := mgr.ListInstances(context.Background())
	if err != nil {
		t.Fatalf("list instances failed: %v", err)
	}
	if len(instList) != 0 {
		t.Fatalf("expected 0 instances after delete, got %d", len(instList))
	}
}

func TestMCWizardSearchEndpoints(t *testing.T) {
	app, st, _, _, _ := setupTestMCServer(t)
	defer st.Close()

	// 1. Modpack search via POST (Datastar v1.0.2 style)
	body, _ := json.Marshal(map[string]string{"packQuery": "fluxw"})
	req := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/modpacks/search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("modpack search POST failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	respBytes, _ := io.ReadAll(resp.Body)
	respStr := string(respBytes)
	if !strings.Contains(respStr, "event: datastar-patch-elements") {
		t.Errorf("expected datastar-patch-elements, got %s", respStr)
	}
	if !strings.Contains(respStr, "selector #wizard-pack-results") {
		t.Errorf("expected selector #wizard-pack-results, got %s", respStr)
	}

	// 2. Mod search via POST (Datastar v1.0.2 style)
	body, _ = json.Marshal(map[string]string{"modQuery": "jei", "mc_version": "1.21.1"})
	req = httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/mods/search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("mods search POST failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	respBytes, _ = io.ReadAll(resp.Body)
	respStr = string(respBytes)
	if !strings.Contains(respStr, "event: datastar-patch-elements") {
		t.Errorf("expected datastar-patch-elements, got %s", respStr)
	}
	if !strings.Contains(respStr, "selector #wizard-mod-results") {
		t.Errorf("expected selector #wizard-mod-results, got %s", respStr)
	}

	// 3. Modpack search via GET
	req = httptest.NewRequest(fiber.MethodGet, "/api/minecraft/wizard/modpacks/search?q=fluxw", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("modpack search GET failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestMCSettingsSave(t *testing.T) {
	app, st, mgr, _, _ := setupTestMCServer(t)
	defer st.Close()

	inst, err := mgr.CreateInstance(context.Background(), domain.Instance{
		Name:      "OldName",
		MCVersion: "1.21.1",
		Tier:      domain.TierSmall,
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	// Test settings save via JSON
	jsonBody, _ := json.Marshal(map[string]string{
		"name":       "NewNameJSON",
		"motd":       "Hello World",
		"tier":       "medium",
		"mc_version": "1.21.1",
	})
	req := httptest.NewRequest(fiber.MethodPost, fmt.Sprintf("/api/minecraft/%d/settings", inst.Number), bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("settings save status = %d, want 200", resp.StatusCode)
	}

	updated, err := mgr.GetInstance(context.Background(), inst.Number)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "NewNameJSON" {
		t.Errorf("got name %q, want NewNameJSON", updated.Name)
	}
	if updated.Tier != domain.TierMedium {
		t.Errorf("got tier %s, want medium", updated.Tier)
	}
}

func TestMCWizardAssembleLoaderSelection(t *testing.T) {
	app, st, mgr, _, _ := setupTestMCServer(t)
	defer st.Close()

	// 1. Attempting assemble create without loader should be rejected
	noLoaderPayload, _ := json.Marshal(map[string]any{
		"name":       "AssembleNoLoader",
		"source":     "assemble",
		"loader":     "",
		"mc_version": "1.21.1",
		"tier":       "small",
	})
	req := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/create", bytes.NewReader(noLoaderPayload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("assemble create request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Please select a mod loader") {
		t.Errorf("expected error toast about selecting a mod loader, got %s", string(body))
	}

	// 2. Assemble create with explicit Fabric loader
	fabricPayload, _ := json.Marshal(map[string]any{
		"name":       "FabricWorld",
		"source":     "assemble",
		"loader":     "fabric",
		"mc_version": "1.21.1",
		"tier":       "small",
	})
	req = httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/create", bytes.NewReader(fabricPayload))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("fabric create request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
	inst1, err := mgr.GetInstance(context.Background(), 1)
	if err != nil {
		t.Fatalf("failed to get instance 1: %v", err)
	}
	if inst1.Loader != domain.LoaderFabric {
		t.Errorf("expected LoaderFabric, got %s", inst1.Loader)
	}

	// 3. Assemble create with explicit NeoForge loader
	neoPayload, _ := json.Marshal(map[string]any{
		"name":       "NeoWorld",
		"source":     "assemble",
		"loader":     "neoforge",
		"mc_version": "1.21.1",
		"tier":       "small",
	})
	req = httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/create", bytes.NewReader(neoPayload))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("neoforge create request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
	inst2, err := mgr.GetInstance(context.Background(), 2)
	if err != nil {
		t.Fatalf("failed to get instance 2: %v", err)
	}
	if inst2.Loader != domain.LoaderNeoForge {
		t.Errorf("expected LoaderNeoForge, got %s", inst2.Loader)
	}

	// 4. Test cart check endpoint
	cartPayload, _ := json.Marshal(map[string]any{
		"cart":       []string{"jei", "waystones"},
		"mc_version": "1.21.1",
	})
	req = httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/cart/check", bytes.NewReader(cartPayload))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("cart check request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("cart check status = %d, want 200", resp.StatusCode)
	}
	cartRespBytes, _ := io.ReadAll(resp.Body)
	cartRespStr := string(cartRespBytes)
	if !strings.Contains(cartRespStr, "datastar-patch-signals") {
		t.Errorf("expected datastar-patch-signals in cart check response, got: %s", cartRespStr)
	}
}
