package wiring_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	"agrelha/internal/domain"
	"agrelha/internal/infra/kube"
	valheimmanifests "agrelha/internal/infra/manifests/valheim"
	k8sruntime "agrelha/internal/infra/runtime/k8s"
	"agrelha/internal/infra/store"
	"agrelha/internal/platform/config"
	"agrelha/internal/wiring"
)

func setupTestValheimServer(t *testing.T) (*fiber.App, *store.Store, *instances.InstanceManager, wiring.Deps, *config.Config) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}

	cs := fake.NewSimpleClientset(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim-odin-01-mods", Namespace: "valheim"},
			Data: map[string]string{
				"mods.txt": "Grantapher-ValheimPlus\n",
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim-odin-01-configs", Namespace: "valheim"},
			Data: map[string]string{
				"valheim_plus.cfg": "[Server]\nenabled = true\n",
			},
		},
	)

	valheimK8s := k8s.NewWithClientset(cs, "valheim", "valheim")
	mgr := instances.NewInstanceManager(
		store.NewValheimInstanceRepo(st), nil, k8sruntime.New(valheimK8s), 16, 4, 2,
		"manifests/valheim", "192.168.20.224",
		valheimmanifests.New("ykhi.xyz/gameserver=true", "valheim"),
		"valheim",
		instances.WithGameID(domain.GameValheim),
		instances.WithBackupsDir(t.TempDir()),
	)

	cfg := &config.Config{
		ValheimNamespace:  "valheim",
		ValheimDeployment: "valheim",
	}

	d := wiring.Deps{
		Store:            st,
		K8s:              valheimK8s,
		ValheimInstances: mgr,
	}
	app := wiring.BuildServer(context.Background(), cfg, d)
	return app, st, mgr, d, cfg
}

func TestValheimDashboardEmpty(t *testing.T) {
	app, st, _, _, _ := setupTestValheimServer(t)
	defer st.Close()

	req := httptest.NewRequest(fiber.MethodGet, "/valheim", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Since AdoptLegacyValheim runs, slot 01 is adopted as "valheim"
	for _, want := range []string{"Valheim Instances", "16 GiB", "valheim", "#01"} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}

func TestValheimWizardPage(t *testing.T) {
	app, st, _, _, _ := setupTestValheimServer(t)
	defer st.Close()

	req := httptest.NewRequest(fiber.MethodGet, "/valheim/create", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	for _, want := range []string{"Create Valheim World", "Step 1: Name Your Valheim Server", "Step 2: Content & Modding", "Tier & Review"} {
		if !strings.Contains(html, want) {
			t.Errorf("wizard missing %q", want)
		}
	}
	if !strings.Contains(html, `/api/valheim/wizard/create`) {
		t.Errorf("wizard missing create @post handler")
	}
	if !strings.Contains(html, `/api/valheim/wizard/import`) {
		t.Errorf("wizard missing import @post handler")
	}
}

func TestValheimWizardCreateAndDetail(t *testing.T) {
	app, st, _, _, _ := setupTestValheimServer(t)
	defer st.Close()

	// 1. Create instance via wizard endpoint (slot 1 is already adopted, so this becomes slot 2)
	payload := map[string]any{
		"name":     "Odin's Hall",
		"password": "secretpassword",
		"seed":     "odinseed",
		"source":   "scratch",
		"tier":     "medium",
		"mods":     []string{"Grantapher-ValheimPlus"},
	}
	payloadBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(fiber.MethodPost, "/api/valheim/wizard/create", bytes.NewReader(payloadBytes))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create status = %d, want 200; body: %s", resp.StatusCode, string(body))
	}
	createBody, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(createBody), `redirect`) {
		t.Errorf("expected redirect signal in wizard response, got: %s", string(createBody))
	}

	// 2. Fetch overview tab for newly created slot 2
	req = httptest.NewRequest(fiber.MethodGet, "/valheim/2/overview", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("get overview failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("overview status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if !strings.Contains(html, "Odin&#39;s Hall") && !strings.Contains(html, "Odin's Hall") {
		t.Errorf("overview missing instance name 'Odin\\'s Hall'")
	}
	if !strings.Contains(html, "6 GiB") {
		t.Errorf("overview missing '6 GiB' medium tier allocation")
	}

	// 3. Test tabs for slot 2
	for _, tab := range []string{"mods", "configs", "console", "backups", "settings"} {
		tabReq := httptest.NewRequest(fiber.MethodGet, fmt.Sprintf("/valheim/2/%s", tab), nil)
		tabResp, tabErr := app.Test(tabReq)
		if tabErr != nil {
			t.Fatalf("get tab %s failed: %v", tab, tabErr)
		}
		if tabResp.StatusCode != fiber.StatusOK {
			t.Fatalf("tab %s status = %d, want 200", tab, tabResp.StatusCode)
		}
	}

	// 4. Test instance shorthand redirect /valheim/1 -> /valheim/1/overview
	reqShort := httptest.NewRequest(fiber.MethodGet, "/valheim/1", nil)
	respShort, err := app.Test(reqShort)
	if err != nil {
		t.Fatalf("shorthand get failed: %v", err)
	}
	if respShort.StatusCode != fiber.StatusFound {
		t.Fatalf("shorthand status = %d, want 302", respShort.StatusCode)
	}
	if loc := respShort.Header.Get("Location"); loc != "/valheim/1/overview" {
		t.Errorf("shorthand location = %q, want /valheim/1/overview", loc)
	}

	// 5. Test legacy backward-compatibility redirects
	reqMods := httptest.NewRequest(fiber.MethodGet, "/mods", nil)
	respMods, err := app.Test(reqMods)
	if err != nil {
		t.Fatalf("mods redirect failed: %v", err)
	}
	if respMods.StatusCode != fiber.StatusTemporaryRedirect {
		t.Fatalf("/mods status = %d, want 307", respMods.StatusCode)
	}
	if loc := respMods.Header.Get("Location"); loc != "/valheim/1/mods" {
		t.Errorf("/mods redirect = %q, want /valheim/1/mods", loc)
	}

	reqConfigs := httptest.NewRequest(fiber.MethodGet, "/configs", nil)
	respConfigs, err := app.Test(reqConfigs)
	if err != nil {
		t.Fatalf("configs redirect failed: %v", err)
	}
	if respConfigs.StatusCode != fiber.StatusTemporaryRedirect {
		t.Fatalf("/configs status = %d, want 307", respConfigs.StatusCode)
	}
	if loc := respConfigs.Header.Get("Location"); loc != "/valheim/1/configs" {
		t.Errorf("/configs redirect = %q, want /valheim/1/configs", loc)
	}
}

func TestValheimLifecycleEndpoints(t *testing.T) {
	app, st, mgr, _, _ := setupTestValheimServer(t)
	defer st.Close()

	// Provision an instance first
	inst, err := mgr.CreateInstance(context.Background(), domain.Instance{
		GameID: domain.GameValheim,
		Name:   "Valhalla",
		Tier:   domain.TierSmall,
	}, "test-admin")
	if err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	// 1. Start
	reqStart := httptest.NewRequest(fiber.MethodPost, fmt.Sprintf("/api/valheim/instances/%d/start", inst.Number), nil)
	respStart, err := app.Test(reqStart)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if respStart.StatusCode != fiber.StatusOK {
		t.Errorf("start status = %d, want 200", respStart.StatusCode)
	}

	// 2. Restart
	reqRestart := httptest.NewRequest(fiber.MethodPost, fmt.Sprintf("/api/valheim/instances/%d/restart", inst.Number), nil)
	respRestart, err := app.Test(reqRestart)
	if err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	if respRestart.StatusCode != fiber.StatusOK {
		t.Errorf("restart status = %d, want 200", respRestart.StatusCode)
	}

	// 3. Stop
	reqStop := httptest.NewRequest(fiber.MethodPost, fmt.Sprintf("/api/valheim/instances/%d/stop", inst.Number), nil)
	respStop, err := app.Test(reqStop)
	if err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	if respStop.StatusCode != fiber.StatusOK {
		t.Errorf("stop status = %d, want 200", respStop.StatusCode)
	}

	// 4. Delete
	reqDelete := httptest.NewRequest(fiber.MethodDelete, fmt.Sprintf("/api/valheim/instances/%d", inst.Number), nil)
	respDelete, err := app.Test(reqDelete)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if respDelete.StatusCode != fiber.StatusOK {
		t.Errorf("delete status = %d, want 200", respDelete.StatusCode)
	}
}
