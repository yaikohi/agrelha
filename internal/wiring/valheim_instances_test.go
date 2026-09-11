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

	// Without active deployment in runtime, no instance is adopted and dashboard is empty
	for _, want := range []string{"Valheim Instances", "16 GiB", "Create your first Valheim world"} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}

func TestValheimNoWorldsRedirects(t *testing.T) {
	app, st, _, _, _ := setupTestValheimServer(t)
	defer st.Close()

	// 1. /valheim/mods and /mods redirect to /valheim
	for _, path := range []string{"/valheim/mods", "/mods"} {
		req := httptest.NewRequest(fiber.MethodGet, path, nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("GET %s failed: %v", path, err)
		}
		if resp.StatusCode != fiber.StatusTemporaryRedirect {
			t.Errorf("GET %s status = %d, want 307", path, resp.StatusCode)
		}
		if loc := resp.Header.Get("Location"); loc != "/valheim" {
			t.Errorf("GET %s location = %q, want /valheim", path, loc)
		}
	}

	// 2. /valheim/configs and /configs redirect to /valheim
	for _, path := range []string{"/valheim/configs", "/configs"} {
		req := httptest.NewRequest(fiber.MethodGet, path, nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("GET %s failed: %v", path, err)
		}
		if resp.StatusCode != fiber.StatusTemporaryRedirect {
			t.Errorf("GET %s status = %d, want 307", path, resp.StatusCode)
		}
		if loc := resp.Header.Get("Location"); loc != "/valheim" {
			t.Errorf("GET %s location = %q, want /valheim", path, loc)
		}
	}

	// 3. Directly attempting instance 1 pages returns 404
	for _, path := range []string{"/valheim/1/overview", "/valheim/1/mods", "/valheim/1/configs"} {
		req := httptest.NewRequest(fiber.MethodGet, path, nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("GET %s failed: %v", path, err)
		}
		if resp.StatusCode != fiber.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, resp.StatusCode)
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

	// 1. Create instance via wizard endpoint (slot 1 is created)
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

	// 2. Fetch overview tab for newly created slot 1
	req = httptest.NewRequest(fiber.MethodGet, "/valheim/1/overview", nil)
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

	// 3. Test tabs for slot 1
	for _, tab := range []string{"mods", "configs", "console", "backups", "settings"} {
		tabReq := httptest.NewRequest(fiber.MethodGet, fmt.Sprintf("/valheim/1/%s", tab), nil)
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

func TestValheimHubCardSymmetry(t *testing.T) {
	app, st, mgr, _, _ := setupTestValheimServer(t)
	defer st.Close()

	// 1. Check 0-worlds empty state on Hub card
	req0 := httptest.NewRequest(fiber.MethodGet, "/", nil)
	resp0, err := app.Test(req0)
	if err != nil {
		t.Fatalf("GET / (0 worlds) failed: %v", err)
	}
	if resp0.StatusCode != fiber.StatusOK {
		t.Fatalf("GET / (0 worlds) status = %d, want 200", resp0.StatusCode)
	}
	body0, _ := io.ReadAll(resp0.Body)
	html0 := string(body0)
	for _, want := range []string{
		"Valheim Worlds",
		"0 of 4 worlds saved",
		"All Valheim worlds are currently offline.",
		"+ Create World",
	} {
		if !strings.Contains(html0, want) {
			t.Errorf("Hub page (0 worlds) missing %q", want)
		}
	}

	// 2. Provision instance 1 and mark it as running so it allocates medium tier (6 GiB) RAM
	inst, err := mgr.CreateInstance(context.Background(), domain.Instance{
		GameID: domain.GameValheim,
		Name:   "Valhalla",
		Tier:   domain.TierMedium,
	}, "test-admin")
	if err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}
	_ = store.NewValheimInstanceRepo(st).UpdateState(inst.Number, domain.StateRunning)

	req := httptest.NewRequest(fiber.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("GET / status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	for _, want := range []string{
		"Valheim Worlds",
		"Dedicated multi-world cluster",
		"1 of 4 worlds saved",
		"1 of 2 running",
		"RAM 6G of 16G",
		"Open Valheim Manager →",
		`href="/valheim"`,
		"Minecraft Worlds",
		"Open Minecraft Manager →",
		`href="/minecraft"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("Hub page missing %q", want)
		}
	}
}

func TestValheimBackupsAndRestores(t *testing.T) {
	app, st, mgr, _, _ := setupTestValheimServer(t)
	defer st.Close()

	// Provision instance 1
	inst, err := mgr.CreateInstance(context.Background(), domain.Instance{
		GameID: domain.GameValheim,
		Name:   "Valheim",
		Tier:   domain.TierMedium,
	}, "test-admin")
	if err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}
	_ = store.NewValheimInstanceRepo(st).UpdateState(inst.Number, domain.StateRunning)

	// 1. Create backup
	reqBkp := httptest.NewRequest(fiber.MethodPost, fmt.Sprintf("/api/valheim/%d/backups/create", inst.Number), nil)
	respBkp, err := app.Test(reqBkp)
	if err != nil {
		t.Fatalf("create backup failed: %v", err)
	}
	if respBkp.StatusCode != fiber.StatusOK {
		t.Fatalf("create backup status = %d, want 200", respBkp.StatusCode)
	}

	// 2. Stop instance 1 before in-place restore
	reqStop := httptest.NewRequest(fiber.MethodPost, fmt.Sprintf("/api/valheim/instances/%d/stop", inst.Number), nil)
	_, _ = app.Test(reqStop)

	// 3. In-place restore
	archiveName := "valheim-valheim-01-daily-123456.tar.gz"
	payloadInPlace := fmt.Sprintf(`{"archive":"%s"}`, archiveName)
	reqRst := httptest.NewRequest(fiber.MethodPost, fmt.Sprintf("/api/valheim/%d/backups/restore-inplace", inst.Number), strings.NewReader(payloadInPlace))
	reqRst.Header.Set("Content-Type", "application/json")
	respRst, err := app.Test(reqRst)
	if err != nil {
		t.Fatalf("in-place restore failed: %v", err)
	}
	if respRst.StatusCode != fiber.StatusOK {
		t.Fatalf("in-place restore status = %d, want 200", respRst.StatusCode)
	}

	// 4. Restore as new world
	payloadNew := fmt.Sprintf(`{"name":"Valheim Cloned","tier":"medium","archive":"%s"}`, archiveName)
	reqRstNew := httptest.NewRequest(fiber.MethodPost, fmt.Sprintf("/api/valheim/%d/backups/restore-new", inst.Number), strings.NewReader(payloadNew))
	reqRstNew.Header.Set("Content-Type", "application/json")
	respRstNew, err := app.Test(reqRstNew)
	if err != nil {
		t.Fatalf("restore-new failed: %v", err)
	}
	if respRstNew.StatusCode != fiber.StatusOK {
		t.Fatalf("restore-new status = %d, want 200", respRstNew.StatusCode)
	}

	// Verify instance 2 was created
	inst2, err := mgr.GetInstance(context.Background(), 2)
	if err != nil || inst2 == nil {
		t.Fatalf("expected instance 2 to be created via restore-new: %v", err)
	}
	if inst2.Name != "Valheim Cloned" {
		t.Errorf("instance 2 name = %q, want 'Valheim Cloned'", inst2.Name)
	}
}
