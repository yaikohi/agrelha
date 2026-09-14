package valheim

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/instances"
	"agrelha/internal/app/modupdates"
	"agrelha/internal/domain"
	valheimmanifests "agrelha/internal/infra/manifests/valheim"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
	"agrelha/internal/web/pages"
)

type mockRefRuntime struct {
	statusFn   func(ref ports.ServerRef) ports.Status
	startErr   error
	stopErr    error
	restartErr error
}

func (m *mockRefRuntime) Start(ctx context.Context, ref ports.ServerRef) error   { return m.startErr }
func (m *mockRefRuntime) Stop(ctx context.Context, ref ports.ServerRef) error    { return m.stopErr }
func (m *mockRefRuntime) Restart(ctx context.Context, ref ports.ServerRef) error { return m.restartErr }
func (m *mockRefRuntime) Status(ctx context.Context, ref ports.ServerRef) (ports.Status, error) {
	if m.statusFn != nil {
		return m.statusFn(ref), nil
	}
	return ports.Status{Lifecycle: ports.LifecycleRunning, Available: true}, nil
}
func (m *mockRefRuntime) Metrics(ctx context.Context, ref ports.ServerRef) (ports.Metrics, error) {
	return ports.Metrics{}, nil
}
func (m *mockRefRuntime) Logs(ctx context.Context, ref ports.ServerRef, opts ports.LogOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockRefRuntime) WatchAvailability(ctx context.Context, ref ports.ServerRef, timeout time.Duration) error {
	return nil
}

type mockValheimReadmeCache struct {
	items  map[string]string
	getErr error
	putErr error
}

func (m *mockValheimReadmeCache) GetReadme(fullName, version string) (string, bool, error) {
	if m.getErr != nil {
		return "", false, m.getErr
	}
	key := fullName + ":" + version
	val, ok := m.items[key]
	return val, ok, nil
}

func (m *mockValheimReadmeCache) PutReadme(fullName, version, markdown string) error {
	if m.putErr != nil {
		return m.putErr
	}
	if m.items == nil {
		m.items = make(map[string]string)
	}
	m.items[fullName+":"+version] = markdown
	return nil
}

type mockAdvValheimCatalog struct {
	mockPackageCatalog
	latestMap map[string]struct {
		ver  string
		deps []string
		err  error
	}
	readmeMap map[string]string
}

func (m *mockAdvValheimCatalog) LatestVersion(ctx context.Context, ns, name string) (string, []string, error) {
	key := ns + "/" + name
	if item, ok := m.latestMap[key]; ok {
		return item.ver, item.deps, item.err
	}
	return m.mockPackageCatalog.LatestVersion(ctx, ns, name)
}

func (m *mockAdvValheimCatalog) Readme(ctx context.Context, ns, name, version string) (string, error) {
	key := ns + "/" + name + ":" + version
	if r, ok := m.readmeMap[key]; ok {
		return r, nil
	}
	return m.mockPackageCatalog.Readme(ctx, ns, name, version)
}

func makeTestR2Z(name string, deps []string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	depList := make([]string, len(deps))
	for i, d := range deps {
		depList[i] = fmt.Sprintf("%q", d)
	}
	manifestJSON := fmt.Sprintf(`{"name": %q, "dependencies": [%s]}`, name, strings.Join(depList, ", "))
	w, _ := zw.Create("manifest.json")
	_, _ = w.Write([]byte(manifestJSON))
	_ = zw.Close()
	return buf.Bytes()
}

func TestValheimHandlerEdges(t *testing.T) {
	// 1. Default Actor
	app := fiber.New()
	h := New(Config{})
	app.Get("/actor", func(c *fiber.Ctx) error {
		return c.SendString(h.cfg.Actor(c))
	})
	app.Get("/actor-custom", func(c *fiber.Ctx) error {
		c.Locals("actor", "viking-admin")
		return c.SendString(h.cfg.Actor(c))
	})
	app.Get("/actor-empty", func(c *fiber.Ctx) error {
		c.Locals("actor", "")
		return c.SendString(h.cfg.Actor(c))
	})

	resp1, _ := app.Test(httptest.NewRequest("GET", "/actor", nil))
	b1, _ := io.ReadAll(resp1.Body)
	if string(b1) != "local" {
		t.Errorf("expected local, got %s", b1)
	}

	resp2, _ := app.Test(httptest.NewRequest("GET", "/actor-custom", nil))
	b2, _ := io.ReadAll(resp2.Body)
	if string(b2) != "viking-admin" {
		t.Errorf("expected viking-admin, got %s", b2)
	}

	resp3, _ := app.Test(httptest.NewRequest("GET", "/actor-empty", nil))
	b3, _ := io.ReadAll(resp3.Body)
	if string(b3) != "local" {
		t.Errorf("expected local, got %s", b3)
	}

	// 2. Default ApplyValheimAfterSync
	h.cfg.ApplyValheimAfterSync("cm", "dep", "key", func(string) bool { return true })

	// 3. /valheim/access redirect to /admins
	appRoutes := fiber.New()
	h.RegisterProtected(appRoutes)
	respAccess, _ := appRoutes.Test(httptest.NewRequest("GET", "/valheim/access", nil))
	if respAccess.StatusCode != fiber.StatusTemporaryRedirect || respAccess.Header.Get("Location") != "/admins" {
		t.Errorf("expected 307 to /admins, got status %d, loc: %s", respAccess.StatusCode, respAccess.Header.Get("Location"))
	}

	// 4. /valheim/:num redirect to /valheim/:num/overview
	respOverview, _ := appRoutes.Test(httptest.NewRequest("GET", "/valheim/10", nil))
	if respOverview.StatusCode != fiber.StatusFound || respOverview.Header.Get("Location") != "/valheim/10/overview" {
		t.Errorf("expected 302 to /valheim/10/overview, got status %d, loc: %s", respOverview.StatusCode, respOverview.Header.Get("Location"))
	}

	// 5. Legacy redirects when ValheimInstances == nil
	appPublic := fiber.New()
	h.RegisterPublic(appPublic)

	for _, path := range []string{"/valheim/mods", "/valheim/configs", "/mods", "/configs"} {
		resp, _ := appRoutes.Test(httptest.NewRequest("GET", path, nil))
		if resp.StatusCode != fiber.StatusTemporaryRedirect || resp.Header.Get("Location") != "/valheim" {
			t.Errorf("expected 307 to /valheim for %s, got status %d, loc: %s", path, resp.StatusCode, resp.Header.Get("Location"))
		}
	}

	for _, path := range []string{"/valheim/mods/export", "/mods/export"} {
		resp, _ := appPublic.Test(httptest.NewRequest("GET", path, nil))
		if resp.StatusCode != fiber.StatusTemporaryRedirect || resp.Header.Get("Location") != "/valheim" {
			t.Errorf("expected 307 to /valheim for %s, got status %d, loc: %s", path, resp.StatusCode, resp.Header.Get("Location"))
		}
	}

	// 6. InstanceStats when ValheimInstances == nil
	if stats := h.InstanceStats(context.Background(), nil); stats != nil {
		t.Errorf("expected nil stats for nil ValheimInstances, got %+v", stats)
	}

	// 7. Full handler: 0 instances redirect, 1 instance redirect, and InstanceStats caching
	hFull, st, mgr, _, _, _, _ := setupTestValheimHandlerFull(t)
	defer st.Close()

	appFull := fiber.New()
	hFull.Register(appFull)

	for _, path := range []string{"/valheim/mods", "/valheim/configs", "/valheim/mods/export"} {
		resp, _ := appFull.Test(httptest.NewRequest("GET", path, nil))
		if resp.StatusCode != fiber.StatusTemporaryRedirect || resp.Header.Get("Location") != "/valheim" {
			t.Errorf("expected 307 to /valheim when 0 instances for %s, got status %d, loc: %s", path, resp.StatusCode, resp.Header.Get("Location"))
		}
	}

	// Create instance 1 (must be 1..4)
	inst, err := mgr.CreateInstance(context.Background(), domain.Instance{
		GameID: domain.GameValheim,
		Number: 1,
		Name:   "Valhalla",
		Tier:   domain.TierMedium,
		State:  domain.StateRunning,
	}, "", "tester")
	if err != nil {
		t.Fatal(err)
	}

	respMods, _ := appFull.Test(httptest.NewRequest("GET", "/valheim/mods", nil))
	if respMods.Header.Get("Location") != "/valheim/1/mods" {
		t.Errorf("expected /valheim/1/mods, got: %s", respMods.Header.Get("Location"))
	}
	respConfigs, _ := appFull.Test(httptest.NewRequest("GET", "/valheim/configs", nil))
	if respConfigs.Header.Get("Location") != "/valheim/1/configs" {
		t.Errorf("expected /valheim/1/configs, got: %s", respConfigs.Header.Get("Location"))
	}
	respExport, _ := appFull.Test(httptest.NewRequest("GET", "/valheim/mods/export", nil))
	if respExport.Header.Get("Location") != "/api/valheim/1/mods/export" {
		t.Errorf("expected /api/valheim/1/mods/export, got: %s", respExport.Header.Get("Location"))
	}

	// InstanceStats caching: call twice within 15s TTL
	stats1 := hFull.InstanceStats(context.Background(), []domain.Instance{*inst})
	stats2 := hFull.InstanceStats(context.Background(), []domain.Instance{*inst})
	if stats1 == nil || stats2 == nil {
		t.Errorf("expected valid stats maps, got stats1=%v, stats2=%v", stats1, stats2)
	}
}

func TestValheimDashboardAllEdges(t *testing.T) {
	// 1. Nil ValheimInstances with LegacyConsole
	hLegacy := New(Config{
		LegacyConsole: func(c *fiber.Ctx) error {
			return c.SendString("legacy-valheim-console")
		},
	})
	appLegacy := fiber.New()
	appLegacy.Get("/valheim", hLegacy.ValheimDashboard)
	respLegacy, _ := appLegacy.Test(httptest.NewRequest("GET", "/valheim", nil))
	bLegacy, _ := io.ReadAll(respLegacy.Body)
	if string(bLegacy) != "legacy-valheim-console" {
		t.Errorf("expected legacy-valheim-console, got %s", bLegacy)
	}

	// 2. Nil ValheimInstances without LegacyConsole
	hNil := New(Config{})
	appNil := fiber.New()
	appNil.Get("/valheim", hNil.ValheimDashboard)
	respNil, _ := appNil.Test(httptest.NewRequest("GET", "/valheim", nil))
	if respNil.StatusCode != fiber.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", respNil.StatusCode)
	}

	// 3. ListInstances error
	stClosed, err := store.Open(filepath.Join(t.TempDir(), "valheim-err.db"))
	if err != nil {
		t.Fatal(err)
	}
	mgrErr := instances.NewInstanceManager(
		store.NewValheimInstanceRepo(stClosed), newMemStateStore(), &mockRuntime{},
		16, 4, 2, "manifests/valheim", "192.168.20.224",
		valheimmanifests.New("ykhi.xyz/gameserver=true", "valheim"), "valheim",
		instances.WithGameID(domain.GameValheim),
	)
	_ = stClosed.Close() // Close to trigger query error

	hErr := New(Config{ValheimInstances: mgrErr})
	appErr := fiber.New()
	appErr.Get("/valheim", hErr.ValheimDashboard)
	respErr, _ := appErr.Test(httptest.NewRequest("GET", "/valheim", nil))
	if respErr.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500, got %d", respErr.StatusCode)
	}

	// 4. Budget blocked reasons in ValheimDashboard
	stBudget, _ := store.Open(filepath.Join(t.TempDir(), "valheim-budget.db"))
	defer stBudget.Close()
	mockState := newMemStateStore()
	rtBudget := &mockRefRuntime{
		statusFn: func(ref ports.ServerRef) ports.Status {
			if strings.Contains(ref.Name, "01") {
				return ports.Status{Lifecycle: ports.LifecycleRunning, Available: true}
			}
			return ports.Status{Lifecycle: ports.LifecycleStopped, Available: false}
		},
	}
	mgrBudget := instances.NewInstanceManager(
		store.NewValheimInstanceRepo(stBudget), mockState, rtBudget,
		4, 4, 1, "manifests/valheim", "192.168.20.224",
		valheimmanifests.New("ykhi.xyz/gameserver=true", "valheim"), "valheim",
		instances.WithGameID(domain.GameValheim),
	)
	// Instance 1: Running (uses 2GiB for TierMedium)
	_, _ = mgrBudget.CreateInstance(context.Background(), domain.Instance{
		GameID: domain.GameValheim,
		Number: 1,
		Name:   "Running World",
		Tier:   domain.TierMedium,
		State:  domain.StateRunning,
	}, "", "tester")
	// Instance 2: Stopped (hits maxRunning=1)
	_, _ = mgrBudget.CreateInstance(context.Background(), domain.Instance{
		GameID: domain.GameValheim,
		Number: 2,
		Name:   "Stopped World",
		Tier:   domain.TierMedium,
		State:  domain.StateStopped,
	}, "", "tester")

	mockCatalog := &mockPackageCatalog{
		results: []domain.ModSearchResult{{Owner: "Author", Name: "Mod", Version: "1.0.0"}},
	}
	checker := modupdates.New(mgrBudget, mockCatalog, modupdates.WithGameID(domain.GameValheim))

	hBudget := New(Config{
		ValheimInstances: mgrBudget,
		ModUpdates:       checker,
	})
	appBudget := fiber.New()
	appBudget.Get("/valheim", hBudget.ValheimDashboard)
	respBudget, err := appBudget.Test(httptest.NewRequest("GET", "/valheim", nil))
	if err != nil || respBudget.StatusCode != fiber.StatusOK {
		t.Fatalf("budget dashboard failed: %v, status: %d", err, respBudget.StatusCode)
	}
	bodyBudget, _ := io.ReadAll(respBudget.Body)
	if !strings.Contains(string(bodyBudget), "Max 1 running instances reached") {
		t.Errorf("expected 'Max 1 running instances reached' in body: %s", string(bodyBudget))
	}

	// RAM budget exceeded branch: maxRunning=4, totalBudgetGiB=3, running instance uses 2GiB, stopped uses 2GiB (2+2 > 3)
	stRAM, _ := store.Open(filepath.Join(t.TempDir(), "valheim-ram.db"))
	defer stRAM.Close()
	mgrRAM := instances.NewInstanceManager(
		store.NewValheimInstanceRepo(stRAM), newMemStateStore(), rtBudget,
		3, 4, 4, "manifests/valheim", "192.168.20.224",
		valheimmanifests.New("ykhi.xyz/gameserver=true", "valheim"), "valheim",
		instances.WithGameID(domain.GameValheim),
	)
	_, _ = mgrRAM.CreateInstance(context.Background(), domain.Instance{
		GameID: domain.GameValheim,
		Number: 1,
		Name:   "Running World",
		Tier:   domain.TierMedium,
		State:  domain.StateRunning,
	}, "", "tester")
	_, _ = mgrRAM.CreateInstance(context.Background(), domain.Instance{
		GameID: domain.GameValheim,
		Number: 2,
		Name:   "Stopped World",
		Tier:   domain.TierMedium,
		State:  domain.StateStopped,
	}, "", "tester")
	hRAM := New(Config{ValheimInstances: mgrRAM})
	appRAM := fiber.New()
	appRAM.Get("/valheim", hRAM.ValheimDashboard)
	respRAM, _ := appRAM.Test(httptest.NewRequest("GET", "/valheim", nil))
	bodyRAM, _ := io.ReadAll(respRAM.Body)
	if !strings.Contains(string(bodyRAM), "Exceeds 3 GiB RAM budget") {
		t.Errorf("expected 'Exceeds 3 GiB RAM budget' in body: %s", string(bodyRAM))
	}

	// 5. Lifecycle Endpoints Error & Success Edges
	stLife, _ := store.Open(filepath.Join(t.TempDir(), "valheim-life.db"))
	defer stLife.Close()
	rtLife := &mockRefRuntime{
		statusFn: func(ref ports.ServerRef) ports.Status {
			if strings.Contains(ref.Name, "02") {
				return ports.Status{Lifecycle: ports.LifecycleRunning, Available: true}
			}
			return ports.Status{Lifecycle: ports.LifecycleStopped, Available: false}
		},
	}
	mgrLife := instances.NewInstanceManager(
		store.NewValheimInstanceRepo(stLife), newMemStateStore(), rtLife,
		16, 4, 4, "manifests/valheim", "192.168.20.224",
		valheimmanifests.New("ykhi.xyz/gameserver=true", "valheim"), "valheim",
		instances.WithGameID(domain.GameValheim),
	)
	// Instance 1: Stopped
	_, _ = mgrLife.CreateInstance(context.Background(), domain.Instance{
		GameID: domain.GameValheim,
		Number: 1,
		Name:   "Stopped World",
		Tier:   domain.TierMedium,
		State:  domain.StateStopped,
	}, "", "tester")
	// Instance 2: Running
	_, _ = mgrLife.CreateInstance(context.Background(), domain.Instance{
		GameID: domain.GameValheim,
		Number: 2,
		Name:   "Running World",
		Tier:   domain.TierMedium,
		State:  domain.StateRunning,
	}, "", "tester")

	hLifecycle := New(Config{ValheimInstances: mgrLife})
	appTest := fiber.New()
	appTest.Post("/test/start/:num", hLifecycle.ValheimInstanceStart)
	appTest.Post("/test/stop/:num", hLifecycle.ValheimInstanceStop)
	appTest.Post("/test/restart/:num", hLifecycle.ValheimInstanceRestart)
	appTest.Delete("/test/delete/:num", hLifecycle.ValheimInstanceDelete)

	// Unparseable instance numbers
	for _, ep := range []string{"/test/start/abc", "/test/stop/abc", "/test/restart/abc", "/test/delete/abc"} {
		method := "POST"
		if strings.Contains(ep, "delete") {
			method = "DELETE"
		}
		resp, _ := appTest.Test(httptest.NewRequest(method, ep, nil))
		b, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(b), "Invalid instance number") {
			t.Errorf("expected Invalid instance number for %s, got: %s", ep, string(b))
		}
	}

	// Unconfigured manager
	hUnconf := New(Config{})
	appUnconfLife := fiber.New()
	appUnconfLife.Post("/test/start/:num", hUnconf.ValheimInstanceStart)
	appUnconfLife.Post("/test/stop/:num", hUnconf.ValheimInstanceStop)
	appUnconfLife.Post("/test/restart/:num", hUnconf.ValheimInstanceRestart)
	appUnconfLife.Delete("/test/delete/:num", hUnconf.ValheimInstanceDelete)

	for _, ep := range []string{"/test/start/1", "/test/stop/1", "/test/restart/1", "/test/delete/1"} {
		method := "POST"
		if strings.Contains(ep, "delete") {
			method = "DELETE"
		}
		resp, _ := appUnconfLife.Test(httptest.NewRequest(method, ep, nil))
		b, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(b), "Valheim instance manager not configured") {
			t.Errorf("expected manager not configured for %s, got: %s", ep, string(b))
		}
	}

	// Runtime errors
	rtLife.startErr = errors.New("start failed boom")
	respStartErr, _ := appTest.Test(httptest.NewRequest("POST", "/test/start/1", nil))
	bStartErr, _ := io.ReadAll(respStartErr.Body)
	if !strings.Contains(string(bStartErr), "start failed boom") {
		t.Errorf("expected start failed toast, got: %s", string(bStartErr))
	}
	rtLife.startErr = nil

	rtLife.stopErr = errors.New("stop failed boom")
	respStopErr, _ := appTest.Test(httptest.NewRequest("POST", "/test/stop/2", nil))
	bStopErr, _ := io.ReadAll(respStopErr.Body)
	if !strings.Contains(string(bStopErr), "stop failed boom") {
		t.Errorf("expected stop failed toast, got: %s", string(bStopErr))
	}
	rtLife.stopErr = nil

	rtLife.restartErr = errors.New("restart failed boom")
	respRestartErr, _ := appTest.Test(httptest.NewRequest("POST", "/test/restart/1", nil))
	bRestartErr, _ := io.ReadAll(respRestartErr.Body)
	if !strings.Contains(string(bRestartErr), "restart failed boom") {
		t.Errorf("expected restart failed toast, got: %s", string(bRestartErr))
	}
	rtLife.restartErr = nil

	// Delete error
	respDelErr, _ := appTest.Test(httptest.NewRequest("DELETE", "/test/delete/99", nil))
	bDelErr, _ := io.ReadAll(respDelErr.Body)
	if !strings.Contains(string(bDelErr), "Failed to delete world") {
		t.Errorf("expected delete failed toast, got: %s", string(bDelErr))
	}

	// Delete success
	respDelOk, _ := appTest.Test(httptest.NewRequest("DELETE", "/test/delete/1", nil))
	bDelOk, _ := io.ReadAll(respDelOk.Body)
	if !strings.Contains(string(bDelOk), "Deleted Valheim instance #01") {
		t.Errorf("expected deleted toast, got: %s", string(bDelOk))
	}
}

func TestValheimConfigsAllEdges(t *testing.T) {
	h, st, mgr, _, memStore, _, _ := setupTestValheimHandlerFull(t)
	defer st.Close()

	_, _ = mgr.CreateInstance(context.Background(), domain.Instance{
		GameID: domain.GameValheim,
		Number: 1,
		Name:   "Config World",
		Tier:   domain.TierMedium,
	}, "", "tester")

	app := fiber.New()
	app.Get("/test/config/:num", h.ValheimInstanceConfigGet)
	app.Post("/test/save/:num", h.ValheimInstanceConfigSave)
	app.Post("/test/delete/:num", h.ValheimInstanceConfigDelete)

	// Unparseable num
	respGetBadNum, _ := app.Test(httptest.NewRequest("GET", "/test/config/bad", nil))
	if respGetBadNum.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400, got %d", respGetBadNum.StatusCode)
	}

	respSaveBadNum, _ := app.Test(httptest.NewRequest("POST", "/test/save/bad", nil))
	bSaveBadNum, _ := io.ReadAll(respSaveBadNum.Body)
	if !strings.Contains(string(bSaveBadNum), "Invalid instance number") {
		t.Errorf("expected Invalid instance number, got: %s", string(bSaveBadNum))
	}

	respDelBadNum, _ := app.Test(httptest.NewRequest("POST", "/test/delete/bad", nil))
	bDelBadNum, _ := io.ReadAll(respDelBadNum.Body)
	if !strings.Contains(string(bDelBadNum), "Invalid instance number") {
		t.Errorf("expected Invalid instance number, got: %s", string(bDelBadNum))
	}

	// ConfigSave JSON body fileName
	saveJSON := `{"file":"custom.cfg","content":"setting=true\r\nother=false"}`
	reqSaveJSON := httptest.NewRequest("POST", "/test/save/1", strings.NewReader(saveJSON))
	reqSaveJSON.Header.Set("Content-Type", "application/json")
	respSaveJSON, _ := app.Test(reqSaveJSON)
	bSaveJSON, _ := io.ReadAll(respSaveJSON.Body)
	if !strings.Contains(string(bSaveJSON), "Saved custom.cfg") {
		t.Errorf("expected saved custom.cfg, got: %s", string(bSaveJSON))
	}

	// ConfigSave FormValue fileName (req.File == "")
	reqSaveForm := httptest.NewRequest("POST", "/test/save/1", strings.NewReader("file=form.cfg&content=a=b"))
	reqSaveForm.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respSaveForm, _ := app.Test(reqSaveForm)
	bSaveForm, _ := io.ReadAll(respSaveForm.Body)
	if !strings.Contains(string(bSaveForm), "Saved form.cfg") {
		t.Errorf("expected saved form.cfg, got: %s", string(bSaveForm))
	}

	// ConfigSave FormValue fallback when req.File is empty in JSON
	reqSaveEmptyJSON := httptest.NewRequest("POST", "/test/save/1?file=fallback.cfg", strings.NewReader(`{"content":"setting=true"}`))
	reqSaveEmptyJSON.Header.Set("Content-Type", "application/json")
	respSaveEmptyJSON, _ := app.Test(reqSaveEmptyJSON)
	bSaveEmptyJSON, _ := io.ReadAll(respSaveEmptyJSON.Body)
	if !strings.Contains(string(bSaveEmptyJSON), "Saved fallback.cfg") {
		t.Errorf("expected saved fallback.cfg, got: %s", string(bSaveEmptyJSON))
	}

	// ConfigDelete empty file name (covers FormValue and Query fallbacks)
	reqDelEmpty := httptest.NewRequest("POST", "/test/delete/1", nil)
	respDelEmpty, _ := app.Test(reqDelEmpty)
	bDelEmpty, _ := io.ReadAll(respDelEmpty.Body)
	if !strings.Contains(string(bDelEmpty), "Invalid config file name") {
		t.Errorf("expected Invalid config file name, got: %s", string(bDelEmpty))
	}

	// ConfigSave patchErr failure
	memStore.patchErr = errors.New("git patch exploded")
	saveFail := `{"file":"custom.cfg","content":"setting=updated"}`
	reqSaveFail := httptest.NewRequest("POST", "/test/save/1", strings.NewReader(saveFail))
	reqSaveFail.Header.Set("Content-Type", "application/json")
	respSaveFail, _ := app.Test(reqSaveFail)
	bSaveFail, _ := io.ReadAll(respSaveFail.Body)
	if !strings.Contains(string(bSaveFail), "Save failed: git patch exploded") {
		t.Errorf("expected save failed toast, got: %s", string(bSaveFail))
	}
	memStore.patchErr = nil

	// ConfigDelete from FormValue (req.File == "")
	reqDelForm := httptest.NewRequest("POST", "/test/delete/1", strings.NewReader("file=form.cfg"))
	reqDelForm.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respDelForm, _ := app.Test(reqDelForm)
	bDelForm, _ := io.ReadAll(respDelForm.Body)
	if !strings.Contains(string(bDelForm), "Deleted form.cfg") {
		t.Errorf("expected deleted form.cfg, got: %s", string(bDelForm))
	}

	// ConfigDelete from query param ?file=custom.cfg
	reqDelQuery := httptest.NewRequest("POST", "/test/delete/1?file=custom.cfg", nil)
	respDelQuery, _ := app.Test(reqDelQuery)
	bDelQuery, _ := io.ReadAll(respDelQuery.Body)
	if !strings.Contains(string(bDelQuery), "Deleted custom.cfg") {
		t.Errorf("expected deleted custom.cfg, got: %s", string(bDelQuery))
	}

	// ConfigDelete patchErr failure (DeleteConfig uses Patch)
	memStore.patchErr = errors.New("git patch delete exploded")
	reqDelFail := httptest.NewRequest("POST", "/test/delete/1?file=custom.cfg", nil)
	respDelFail, _ := app.Test(reqDelFail)
	bDelFail, _ := io.ReadAll(respDelFail.Body)
	if !strings.Contains(string(bDelFail), "Delete failed: git patch delete exploded") {
		t.Errorf("expected delete failed toast, got: %s", string(bDelFail))
	}
	memStore.patchErr = nil

	// ConfigGet patchSignals error branch
	origPatch := patchSignals
	patchSignals = func(w *bufio.Writer, s any) error {
		return errors.New("patchSignals failed")
	}
	t.Cleanup(func() {
		patchSignals = origPatch
	})

	reqGetSignalErr := httptest.NewRequest("GET", "/test/config/1?f=custom.cfg", nil)
	respGetSignalErr, _ := app.Test(reqGetSignalErr)
	if respGetSignalErr.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500 on patchSignals error, got: %d", respGetSignalErr.StatusCode)
	}
	patchSignals = origPatch
}

func TestValheimDetailAllEdges(t *testing.T) {
	h, st, mgr, _, memStore, mockTS, bDir := setupTestValheimHandlerFull(t)
	defer st.Close()

	// Create a backup file in bDir matching pattern valheim-modded-world-01-*.tar.gz
	_ = os.WriteFile(filepath.Join(bDir, "valheim-modded-world-01-20260914.tar.gz"), []byte("data"), 0644)

	// Create modded instance (num 1)
	inst, err := mgr.CreateInstance(context.Background(), domain.Instance{
		GameID:   domain.GameValheim,
		Number:   1,
		Name:     "Modded World",
		Password: "password1",
		Tier:     domain.TierMedium,
		State:    domain.StateRunning,
		Source:   domain.SourceModlist,
	}, "Smoothbrain-Mining-1.2.0\n", "tester")
	if err != nil {
		t.Fatal(err)
	}

	// Create vanilla instance (num 2)
	_, err = mgr.CreateInstance(context.Background(), domain.Instance{
		GameID:   domain.GameValheim,
		Number:   2,
		Name:     "Vanilla World",
		Password: "password2",
		Tier:     domain.TierSmall,
		State:    domain.StateStopped,
		Source:   domain.SourceVanilla,
	}, "", "tester")
	if err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	h.Register(app)
	app.Get("/test/page/:num/:tab?", h.ValheimInstancePage)
	app.Post("/test/settings/:num", h.ValheimInstanceSettingsSave)
	app.Post("/test/remove/:num", h.ValheimInstanceModsRemove)
	app.Post("/test/install/:num", h.ValheimInstanceModsInstall)
	app.Get("/test/search/:num", h.ValheimInstanceModsSearch)
	app.Post("/test/search/:num", h.ValheimInstanceModsSearch)
	app.Get("/test/moddetail/:num", h.ValheimModDetail)
	app.Post("/test/moddetail/:num", h.ValheimModDetail)
	app.Get("/test/export/:num", h.ValheimInstanceExport)

	// 1. ValheimInstancePage edges
	// Unconfigured
	hUnconf := New(Config{})
	appUnconf := fiber.New()
	appUnconf.Get("/test/page/:num/:tab?", hUnconf.ValheimInstancePage)
	respUnconfPage, _ := appUnconf.Test(httptest.NewRequest("GET", "/test/page/1", nil))
	if respUnconfPage.StatusCode != fiber.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", respUnconfPage.StatusCode)
	}

	// Bad num
	respBadNumPage, _ := app.Test(httptest.NewRequest("GET", "/test/page/bad", nil))
	if respBadNumPage.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400, got %d", respBadNumPage.StatusCode)
	}

	// Empty tab defaults to overview
	respEmptyTab, _ := app.Test(httptest.NewRequest("GET", "/test/page/1", nil))
	if respEmptyTab.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200, got %d", respEmptyTab.StatusCode)
	}

	// Whitespace tab defaults to overview
	appUnescape := fiber.New(fiber.Config{UnescapePath: true})
	appUnescape.Get("/test/page/:num/:tab", h.ValheimInstancePage)
	respSpaceTab, _ := appUnescape.Test(httptest.NewRequest("GET", "/test/page/1/%20", nil))
	if respSpaceTab.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for whitespace tab, got: %d", respSpaceTab.StatusCode)
	}

	// Not found
	respNotFoundPage, _ := app.Test(httptest.NewRequest("GET", "/test/page/99", nil))
	if respNotFoundPage.StatusCode != fiber.StatusNotFound {
		t.Errorf("expected 404, got %d", respNotFoundPage.StatusCode)
	}

	// LastIncident error branch
	h.cfg.LastIncident = func(ctx context.Context, number int) (*domain.Incident, error) {
		return nil, errors.New("incident db error")
	}
	respIncErr, _ := app.Test(httptest.NewRequest("GET", "/test/page/1/overview", nil))
	if respIncErr.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200, got %d", respIncErr.StatusCode)
	}
	h.cfg.LastIncident = nil

	// Vanilla instance on mods tab (skips ModUpdateState)
	respVanillaMods, _ := app.Test(httptest.NewRequest("GET", "/test/page/2/mods", nil))
	if respVanillaMods.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200, got %d", respVanillaMods.StatusCode)
	}

	// Configs tab
	respConfigsTab, _ := app.Test(httptest.NewRequest("GET", "/test/page/1/configs", nil))
	if respConfigsTab.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200, got %d", respConfigsTab.StatusCode)
	}

	// Backups tab
	respBackupsTab, _ := app.Test(httptest.NewRequest("GET", "/test/page/1/backups", nil))
	if respBackupsTab.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200, got %d", respBackupsTab.StatusCode)
	}

	// 2. SettingsSave edges
	// Unconfigured
	appUnconf.Post("/test/settings/:num", hUnconf.ValheimInstanceSettingsSave)
	respUnconfSettings, _ := appUnconf.Test(httptest.NewRequest("POST", "/test/settings/1", nil))
	bUnconfSettings, _ := io.ReadAll(respUnconfSettings.Body)
	if !strings.Contains(string(bUnconfSettings), "unconfigured") {
		t.Errorf("expected unconfigured, got: %s", string(bUnconfSettings))
	}

	// Bad num
	respBadNumSettings, _ := app.Test(httptest.NewRequest("POST", "/test/settings/bad", nil))
	bBadNumSettings, _ := io.ReadAll(respBadNumSettings.Body)
	if !strings.Contains(string(bBadNumSettings), "Invalid instance number") {
		t.Errorf("expected Invalid instance number, got: %s", string(bBadNumSettings))
	}

	// JSON body settings
	settingsJSON := `{"name":"New World Name","password":"newpassword","tier":"small"}`
	reqJSONSettings := httptest.NewRequest("POST", "/test/settings/1", strings.NewReader(settingsJSON))
	reqJSONSettings.Header.Set("Content-Type", "application/json")
	respJSONSettings, _ := app.Test(reqJSONSettings)
	bJSONSettings, _ := io.ReadAll(respJSONSettings.Body)
	if !strings.Contains(string(bJSONSettings), "Settings saved successfully") {
		t.Errorf("expected Settings saved successfully, got: %s", string(bJSONSettings))
	}

	// FormValue settings (name, password, tier fallback)
	formSettings := url.Values{
		"name":     {"Form Name"},
		"password": {"formpwd"},
		"tier":     {"medium"},
	}.Encode()
	reqFormSettings := httptest.NewRequest("POST", "/test/settings/1", strings.NewReader(formSettings))
	reqFormSettings.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respFormSettings, _ := app.Test(reqFormSettings)
	bFormSettings, _ := io.ReadAll(respFormSettings.Body)
	if !strings.Contains(string(bFormSettings), "Settings saved successfully") {
		t.Errorf("expected Settings saved successfully, got: %s", string(bFormSettings))
	}

	// FormValue fallback for settings when req fields are empty in JSON
	reqSettingsFallback := httptest.NewRequest("POST", "/test/settings/1?name=FallbackName&password=fallbackpwd&tier=medium", strings.NewReader(`{}`))
	reqSettingsFallback.Header.Set("Content-Type", "application/json")
	respSettingsFallback, _ := app.Test(reqSettingsFallback)
	bSettingsFallback, _ := io.ReadAll(respSettingsFallback.Body)
	if !strings.Contains(string(bSettingsFallback), "Settings saved successfully") {
		t.Errorf("expected Settings saved successfully, got: %s", string(bSettingsFallback))
	}

	// 3. ModsRemove and ModsInstall edges
	appUnconf.Post("/test/remove/:num", hUnconf.ValheimInstanceModsRemove)
	appUnconf.Post("/test/install/:num", hUnconf.ValheimInstanceModsInstall)

	// Unconfigured
	respUnconfRemove, _ := appUnconf.Test(httptest.NewRequest("POST", "/test/remove/1", nil))
	bUnconfRemove, _ := io.ReadAll(respUnconfRemove.Body)
	if !strings.Contains(string(bUnconfRemove), "unconfigured") {
		t.Errorf("expected unconfigured, got: %s", string(bUnconfRemove))
	}

	respUnconfInstall, _ := appUnconf.Test(httptest.NewRequest("POST", "/test/install/1", nil))
	bUnconfInstall, _ := io.ReadAll(respUnconfInstall.Body)
	if !strings.Contains(string(bUnconfInstall), "unconfigured") {
		t.Errorf("expected unconfigured, got: %s", string(bUnconfInstall))
	}

	// Bad num
	respBadNumRemove, _ := app.Test(httptest.NewRequest("POST", "/test/remove/bad", nil))
	bBadNumRemove, _ := io.ReadAll(respBadNumRemove.Body)
	if !strings.Contains(string(bBadNumRemove), "Invalid instance number") {
		t.Errorf("expected Invalid instance number, got: %s", string(bBadNumRemove))
	}

	respBadNumInstall, _ := app.Test(httptest.NewRequest("POST", "/test/install/bad", nil))
	bBadNumInstall, _ := io.ReadAll(respBadNumInstall.Body)
	if !strings.Contains(string(bBadNumInstall), "Invalid instance number") {
		t.Errorf("expected Invalid instance number, got: %s", string(bBadNumInstall))
	}

	// Install / Remove slug from FormValue
	reqFormInstall := httptest.NewRequest("POST", "/test/install/1", strings.NewReader("slug=Smoothbrain-Mining"))
	reqFormInstall.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respFormInstall, _ := app.Test(reqFormInstall)
	bFormInstall, _ := io.ReadAll(respFormInstall.Body)
	if !strings.Contains(string(bFormInstall), "installed-mods-container") {
		t.Errorf("expected installed-mods-container in response: %s", string(bFormInstall))
	}

	reqFormRemove := httptest.NewRequest("POST", "/test/remove/1", strings.NewReader("slug=Smoothbrain-Mining"))
	reqFormRemove.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respFormRemove, _ := app.Test(reqFormRemove)
	bFormRemove, _ := io.ReadAll(respFormRemove.Body)
	if !strings.Contains(string(bFormRemove), "installed-mods-container") {
		t.Errorf("expected installed-mods-container in response: %s", string(bFormRemove))
	}

	// Install on vanilla instance (fails with VanillaImmutableErr)
	reqVanillaInstall := httptest.NewRequest("POST", "/test/install/2?slug=Smoothbrain-Mining", nil)
	respVanillaInstall, _ := app.Test(reqVanillaInstall)
	bVanillaInstall, _ := io.ReadAll(respVanillaInstall.Body)
	if !strings.Contains(string(bVanillaInstall), "Install failed") {
		t.Errorf("expected Install failed for vanilla instance, got: %s", string(bVanillaInstall))
	}

	// Remove error when state store patch fails
	memStore.patchErr = errors.New("cannot remove mod")
	reqRemoveErr := httptest.NewRequest("POST", "/test/remove/1?slug=Smoothbrain-Mining", nil)
	respRemoveErr, _ := app.Test(reqRemoveErr)
	bRemoveErr, _ := io.ReadAll(respRemoveErr.Body)
	if !strings.Contains(string(bRemoveErr), "Remove failed: cannot remove mod") {
		t.Errorf("expected Remove failed toast, got: %s", string(bRemoveErr))
	}
	memStore.patchErr = nil

	// 4. ModsSearch edges
	// Bad num
	respBadSearchNum, _ := app.Test(httptest.NewRequest("GET", "/test/search/bad", nil))
	if respBadSearchNum.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400, got %d", respBadSearchNum.StatusCode)
	}

	// Unconfigured TS
	hNoTS := New(Config{ValheimInstances: mgr})
	appNoTS := fiber.New()
	appNoTS.Get("/test/search/:num", hNoTS.ValheimInstanceModsSearch)
	respNoTSSearch, _ := appNoTS.Test(httptest.NewRequest("GET", "/test/search/1", nil))
	bNoTSSearch, _ := io.ReadAll(respNoTSSearch.Body)
	if !strings.Contains(string(bNoTSSearch), "Thunderstore catalog unconfigured") {
		t.Errorf("expected catalog unconfigured, got: %s", string(bNoTSSearch))
	}

	// TS Search error
	mockTS.searchErr = errors.New("thunderstore offline")
	respTSErr, _ := app.Test(httptest.NewRequest("GET", "/test/search/1?q=valheim", nil))
	bTSErr, _ := io.ReadAll(respTSErr.Body)
	if !strings.Contains(string(bTSErr), "Search failed: thunderstore offline") {
		t.Errorf("expected search failed, got: %s", string(bTSErr))
	}
	mockTS.searchErr = nil

	// TS Search empty results
	mockTS.results = nil
	respEmptyResults, _ := app.Test(httptest.NewRequest("GET", "/test/search/1?q=notfound", nil))
	bEmptyResults, _ := io.ReadAll(respEmptyResults.Body)
	if !strings.Contains(string(bEmptyResults), "No Thunderstore mods found matching your search") {
		t.Errorf("expected no mods found, got: %s", string(bEmptyResults))
	}

	// TS Search with results >= limit (trigger Load more mods) and different query params
	mockTS.results = []domain.ModSearchResult{
		{Owner: "Author1", Name: "Mod1", Version: "1.0.0", Description: "Description 1", Downloads: 2500000},
		{Owner: "Author2", Name: "Mod2", Version: "2.0.0", Description: "Description 2", Downloads: 500, Icon: "https://icon.png"},
	}

	// Test query from modSearch, modQuery, q
	for _, bodyJSON := range []string{
		`{"modSearch":"test","limit":2}`,
		`{"modQuery":"test","limit":2}`,
		`{"q":"test","limit":2}`,
	} {
		reqQ := httptest.NewRequest("POST", "/test/search/1", strings.NewReader(bodyJSON))
		reqQ.Header.Set("Content-Type", "application/json")
		respQ, _ := app.Test(reqQ)
		bQ, _ := io.ReadAll(respQ.Body)
		if !strings.Contains(string(bQ), "Load more mods...") {
			t.Errorf("expected Load more mods button for %s, got: %s", bodyJSON, string(bQ))
		}
	}

	// Query limit (c.Query("limit") > 0)
	respQueryLimit, _ := app.Test(httptest.NewRequest("GET", "/test/search/1?limit=10", nil))
	if respQueryLimit.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200, got %d", respQueryLimit.StatusCode)
	}

	// Popular header (q == "") and installed mod check in search results
	_, _ = mgr.InstallMod(context.Background(), 1, "Smoothbrain-Mining", "test")
	mockTS.results = []domain.ModSearchResult{
		{Owner: "Smoothbrain", Name: "Mining", Version: "1.2.0", Description: "Mining progression"},
	}
	respSearchInstalled, _ := app.Test(httptest.NewRequest("GET", "/test/search/1", nil))
	bSearchInstalled, _ := io.ReadAll(respSearchInstalled.Body)
	if !strings.Contains(string(bSearchInstalled), "Installed") || !strings.Contains(string(bSearchInstalled), "Popular Community Mods") {
		t.Errorf("expected Installed badge and Popular header in search results, got: %s", string(bSearchInstalled))
	}

	// 5. ValheimModDetail edges
	// Bad num
	respBadDetailNum, _ := app.Test(httptest.NewRequest("GET", "/test/moddetail/bad", nil))
	if respBadDetailNum.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400, got %d", respBadDetailNum.StatusCode)
	}

	// Missing slug
	respNoSlugDetail, _ := app.Test(httptest.NewRequest("GET", "/test/moddetail/1", nil))
	bNoSlugDetail, _ := io.ReadAll(respNoSlugDetail.Body)
	if !strings.Contains(string(bNoSlugDetail), "Mod slug required") {
		t.Errorf("expected Mod slug required, got: %s", string(bNoSlugDetail))
	}

	// Detail when TS is nil
	appNoTS.Get("/test/moddetail/:num", hNoTS.ValheimModDetail)
	respNoTSDetail, _ := appNoTS.Test(httptest.NewRequest("GET", "/test/moddetail/1?slug=Denikson-BepInExPack", nil))
	bNoTSDetail, _ := io.ReadAll(respNoTSDetail.Body)
	if !strings.Contains(string(bNoTSDetail), "Denikson") {
		t.Errorf("expected Denikson in detail drawer, got: %s", string(bNoTSDetail))
	}

	// Detail with ReadmeCache hit and miss
	readmeCache := &mockValheimReadmeCache{}
	h.cfg.ReadmeCache = readmeCache
	// Miss: populates cache
	reqMiss := httptest.NewRequest("GET", "/test/moddetail/1?slug=Author1/Mod1", nil)
	respMiss, _ := app.Test(reqMiss)
	bMiss, _ := io.ReadAll(respMiss.Body)
	if !strings.Contains(string(bMiss), "Awesome Valheim mod") {
		t.Errorf("expected readme in detail, got: %s", string(bMiss))
	}
	// Hit: serves from cache
	reqHit := httptest.NewRequest("GET", "/test/moddetail/1?slug=Author1/Mod1", nil)
	respHit, _ := app.Test(reqHit)
	bHit, _ := io.ReadAll(respHit.Body)
	if !strings.Contains(string(bHit), "Awesome Valheim mod") {
		t.Errorf("expected readme from cache, got: %s", string(bHit))
	}

	// Detail with Readme error
	mockTS.readmeErr = errors.New("readme not found")
	readmeCache.items = nil // clear cache
	reqReadmeErr := httptest.NewRequest("GET", "/test/moddetail/1?slug=Author2-Mod2", nil)
	respReadmeErr, _ := app.Test(reqReadmeErr)
	bReadmeErr, _ := io.ReadAll(respReadmeErr.Body)
	if !strings.Contains(string(bReadmeErr), "No README available") {
		t.Errorf("expected No README available, got: %s", string(bReadmeErr))
	}
	mockTS.readmeErr = nil

	// LatestVersion error
	mockTS.latestErr = errors.New("version lookup failed")
	reqLatestErr := httptest.NewRequest("GET", "/test/moddetail/1?slug=Author2-Mod2", nil)
	respLatestErr, _ := app.Test(reqLatestErr)
	bLatestErr, _ := io.ReadAll(respLatestErr.Body)
	if !strings.Contains(string(bLatestErr), "Author2") {
		t.Errorf("expected Author2 in output, got: %s", string(bLatestErr))
	}
	mockTS.latestErr = nil

	// Test installed mod in ValheimModDetail (triggers isInstalled = true, Remove Mod, and deps)
	reqInstalledDetail := httptest.NewRequest("GET", "/test/moddetail/1?slug=Smoothbrain-Mining", nil)
	respInstalledDetail, _ := app.Test(reqInstalledDetail)
	bInstalledDetail, _ := io.ReadAll(respInstalledDetail.Body)
	if !strings.Contains(string(bInstalledDetail), "Remove Mod") || !strings.Contains(string(bInstalledDetail), "Required Dependencies") {
		t.Errorf("expected Remove Mod and Required Dependencies, got: %s", string(bInstalledDetail))
	}

	// Test OnlySlug in ValheimModDetail (triggers TS.Get(slug) and version == "")
	mockTS.results = append(mockTS.results, domain.ModSearchResult{
		Owner:       "OnlySlugAuthor",
		Name:        "OnlySlugMod",
		Version:     "",
		Description: "Only slug mod",
	})
	reqOnlySlugDetail := httptest.NewRequest("GET", "/test/moddetail/1?slug=OnlySlugMod", nil)
	respOnlySlugDetail, _ := app.Test(reqOnlySlugDetail)
	bOnlySlugDetail, _ := io.ReadAll(respOnlySlugDetail.Body)
	if !strings.Contains(string(bOnlySlugDetail), "OnlySlugAuthor") {
		t.Errorf("expected OnlySlugAuthor, got: %s", string(bOnlySlugDetail))
	}

	// Seam errors in ValheimModDetail
	origInner := innerElement
	origPatch := patchSignals
	t.Cleanup(func() {
		innerElement = origInner
		patchSignals = origPatch
	})

	innerElement = func(w *bufio.Writer, sel, el string) error {
		return errors.New("innerElement failed")
	}
	respInnerErr, _ := app.Test(httptest.NewRequest("GET", "/test/moddetail/1?slug=Author1-Mod1", nil))
	if respInnerErr.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500 on innerElement error, got: %d", respInnerErr.StatusCode)
	}

	innerElement = origInner
	patchSignals = func(w *bufio.Writer, s any) error {
		return errors.New("patchSignals failed")
	}
	respPatchErr, _ := app.Test(httptest.NewRequest("GET", "/test/moddetail/1?slug=Author1-Mod1", nil))
	if respPatchErr.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500 on patchSignals error, got: %d", respPatchErr.StatusCode)
	}
	patchSignals = origPatch

	// 6. Helpers
	// parseSlug
	p1O, p1N := parseSlug("custom/mod")
	if p1O != "custom" || p1N != "mod" {
		t.Errorf("parseSlug slash failed: %s, %s", p1O, p1N)
	}
	p2O, p2N := parseSlug("custom-mod")
	if p2O != "custom" || p2N != "mod" {
		t.Errorf("parseSlug dash failed: %s, %s", p2O, p2N)
	}
	p3O, p3N := parseSlug("baremod")
	if p3O != "denikson" || p3N != "baremod" {
		t.Errorf("parseSlug bare failed: %s, %s", p3O, p3N)
	}

	// formatDownloads
	if fd := formatDownloads(2500000); fd != "2.5M" {
		t.Errorf("expected 2.5M, got %s", fd)
	}
	if fd := formatDownloads(12500); fd != "12.5k" {
		t.Errorf("expected 12.5k, got %s", fd)
	}
	if fd := formatDownloads(800); fd != "800" {
		t.Errorf("expected 800, got %s", fd)
	}

	// prettyDeps
	deps := prettyDeps([]string{"short", "denikson-bepinexpack_valheim-5.4.2202", "author-coolmod-1.0.0"})
	if len(deps) != 1 || deps[0] != "coolmod" {
		t.Errorf("expected [coolmod], got %v", deps)
	}

	// statusBadge
	if sb := statusBadge(true); !strings.Contains(sb, "Installed in this world") {
		t.Errorf("expected Installed in this world, got %s", sb)
	}
	if sb := statusBadge(false); !strings.Contains(sb, "Available to install") {
		t.Errorf("expected Available to install, got %s", sb)
	}

	// renderInstalledModsHTML
	emptyModsHTML := renderInstalledModsHTML(1, nil)
	if !strings.Contains(emptyModsHTML, "No custom mods installed") {
		t.Errorf("expected No custom mods installed, got %s", emptyModsHTML)
	}

	filledModsHTML := renderInstalledModsHTML(1, []string{"Author-Mod", "Author-Pinned-1.0.0"})
	if !strings.Contains(filledModsHTML, "Author") {
		t.Errorf("expected Author in table, got %s", filledModsHTML)
	}

	// ssePatchElements with seam error
	innerElement = func(w *bufio.Writer, sel, el string) error {
		return errors.New("innerElement failed")
	}
	respPatchSeamErr, _ := app.Test(httptest.NewRequest("GET", "/test/search/1?q=valheim", nil))
	if respPatchSeamErr.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500 on ssePatchElements error, got %d", respPatchSeamErr.StatusCode)
	}
	innerElement = origInner

	// 7. ValheimInstanceExport edges
	// Bad num
	respBadExportNum, _ := app.Test(httptest.NewRequest("GET", "/test/export/bad", nil))
	if respBadExportNum.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400, got %d", respBadExportNum.StatusCode)
	}

	// Unconfigured
	appUnconf.Get("/test/export/:num", hUnconf.ValheimInstanceExport)
	respUnconfExport, _ := appUnconf.Test(httptest.NewRequest("GET", "/test/export/1", nil))
	if respUnconfExport.StatusCode != fiber.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", respUnconfExport.StatusCode)
	}

	// ValheimGame is nil
	hNoGame := New(Config{ValheimInstances: mgr})
	appNoGame := fiber.New()
	appNoGame.Get("/test/export/:num", hNoGame.ValheimInstanceExport)
	respNoGameExport, _ := appNoGame.Test(httptest.NewRequest("GET", "/test/export/1", nil))
	if respNoGameExport.StatusCode != fiber.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", respNoGameExport.StatusCode)
	}

	// Instance not found
	mockGame := &mockValheimGame{
		bundle: domain.Bundle{Filename: "export.r2z", ContentType: "application/zip", Data: []byte("zip")},
	}
	h.cfg.ValheimGame = mockGame
	respNotFoundExport, _ := app.Test(httptest.NewRequest("GET", "/test/export/99", nil))
	if respNotFoundExport.StatusCode != fiber.StatusNotFound {
		t.Errorf("expected 404, got %d", respNotFoundExport.StatusCode)
	}

	// ExportClientBundle error
	mockGame.exportErr = errors.New("export bundle build failure")
	respExportErr, _ := app.Test(httptest.NewRequest("GET", "/test/export/1", nil))
	if respExportErr.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500, got %d", respExportErr.StatusCode)
	}
	mockGame.exportErr = nil

	_ = inst
}

func TestValheimWizardAllEdges(t *testing.T) {
	h, st, _, _, _, mockTS, _ := setupTestValheimHandlerFull(t)
	defer st.Close()

	app := fiber.New()
	app.Get("/valheim/create", h.ValheimWizardPage)
	app.Post("/api/valheim/wizard/mods/search", h.ValheimWizardModsSearch)
	app.Get("/api/valheim/wizard/mods/search", h.ValheimWizardModsSearch)
	app.Post("/api/valheim/wizard/mods/detail", h.ValheimWizardModDetail)
	app.Get("/api/valheim/wizard/mods/detail", h.ValheimWizardModDetail)
	app.Post("/api/valheim/wizard/cart/sync", h.ValheimWizardCartSync)
	app.Post("/api/valheim/wizard/import", h.ValheimWizardImport)
	app.Post("/api/valheim/wizard/create", h.ValheimWizardCreate)

	// 1. WizardPage edges
	// Unconfigured
	hUnconf := New(Config{})
	appUnconf := fiber.New()
	appUnconf.Get("/valheim/create", hUnconf.ValheimWizardPage)
	respUnconfWizard, _ := appUnconf.Test(httptest.NewRequest("GET", "/valheim/create", nil))
	if respUnconfWizard.StatusCode != fiber.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", respUnconfWizard.StatusCode)
	}

	// ListInstances error
	stErr, _ := store.Open(filepath.Join(t.TempDir(), "valheim-wizerr.db"))
	mgrWizErr := instances.NewInstanceManager(
		store.NewValheimInstanceRepo(stErr), newMemStateStore(), &mockRuntime{},
		16, 4, 2, "manifests/valheim", "192.168.20.224",
		valheimmanifests.New("ykhi.xyz/gameserver=true", "valheim"), "valheim",
		instances.WithGameID(domain.GameValheim),
	)
	_ = stErr.Close()
	hWizErr := New(Config{ValheimInstances: mgrWizErr})
	appWizErr := fiber.New()
	appWizErr.Get("/valheim/create", hWizErr.ValheimWizardPage)
	respWizErr, _ := appWizErr.Test(httptest.NewRequest("GET", "/valheim/create", nil))
	if respWizErr.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500, got %d", respWizErr.StatusCode)
	}

	// 2. WizardModsSearch edges
	// TS unconfigured with JSON and SSE
	appUnconf.Get("/api/valheim/wizard/mods/search", hUnconf.ValheimWizardModsSearch)
	reqUnconfJSON := httptest.NewRequest("GET", "/api/valheim/wizard/mods/search", nil)
	reqUnconfJSON.Header.Set("Accept", "application/json")
	respUnconfJSON, _ := appUnconf.Test(reqUnconfJSON)
	bUnconfJSON, _ := io.ReadAll(respUnconfJSON.Body)
	if string(bUnconfJSON) != "[]" {
		t.Errorf("expected [], got %s", bUnconfJSON)
	}

	reqUnconfSSE := httptest.NewRequest("GET", "/api/valheim/wizard/mods/search", nil)
	respUnconfSSE, _ := appUnconf.Test(reqUnconfSSE)
	bUnconfSSE, _ := io.ReadAll(respUnconfSSE.Body)
	if !strings.Contains(string(bUnconfSSE), "Thunderstore catalog unconfigured") {
		t.Errorf("expected unconfigured, got: %s", string(bUnconfSSE))
	}

	// TS Search error with JSON and SSE
	mockTS.searchErr = errors.New("search error")
	reqErrJSON := httptest.NewRequest("GET", "/api/valheim/wizard/mods/search", nil)
	reqErrJSON.Header.Set("Accept", "application/json")
	respErrJSON, _ := app.Test(reqErrJSON)
	if respErrJSON.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500, got %d", respErrJSON.StatusCode)
	}

	reqErrSSE := httptest.NewRequest("GET", "/api/valheim/wizard/mods/search", nil)
	respErrSSE, _ := app.Test(reqErrSSE)
	bErrSSE, _ := io.ReadAll(respErrSSE.Body)
	if !strings.Contains(string(bErrSSE), "Search failed") {
		t.Errorf("expected Search failed, got: %s", string(bErrSSE))
	}
	mockTS.searchErr = nil

	// Empty results
	mockTS.results = nil
	respWizEmpty, _ := app.Test(httptest.NewRequest("GET", "/api/valheim/wizard/mods/search?q=nothing", nil))
	bWizEmpty, _ := io.ReadAll(respWizEmpty.Body)
	if !strings.Contains(string(bWizEmpty), "No Thunderstore mods found") {
		t.Errorf("expected No Thunderstore mods found, got: %s", string(bWizEmpty))
	}

	// Results with different params: wizardModSearch, modSearch, modQuery, q
	mockTS.results = []domain.ModSearchResult{
		{Owner: "Viking", Name: "Shield", Version: "1.0.0", Description: "Shield mod", Downloads: 1000},
		{Owner: "Viking", Name: "Sword", Version: "1.1.0", Description: "Sword mod", Downloads: 50, Icon: "icon.png"},
	}

	for _, payload := range []string{
		`{"wizardModSearch":"viking","limit":2}`,
		`{"modSearch":"viking","limit":2}`,
		`{"modQuery":"viking","limit":2}`,
		`{"q":"viking","limit":2}`,
	} {
		reqP := httptest.NewRequest("POST", "/api/valheim/wizard/mods/search", strings.NewReader(payload))
		reqP.Header.Set("Content-Type", "application/json")
		respP, _ := app.Test(reqP)
		bP, _ := io.ReadAll(respP.Body)
		if !strings.Contains(string(bP), "Load more mods...") {
			t.Errorf("expected Load more mods for %s, got: %s", payload, string(bP))
		}
	}

	// Limit from query parameter (?limit=10)
	respWizLimitQuery, _ := app.Test(httptest.NewRequest("GET", "/api/valheim/wizard/mods/search?limit=10", nil))
	if respWizLimitQuery.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for wizard search limit query, got: %d", respWizLimitQuery.StatusCode)
	}

	// 3. CartSync and renderWizardCartHTML
	// Empty cart
	if hEmpty := renderWizardCartHTML(nil); !strings.Contains(hEmpty, "No mods added yet") {
		t.Errorf("expected No mods added yet, got: %s", hEmpty)
	}

	// CartSync with comma-separated FormValue and duplicates
	syncForm := url.Values{"cart": []string{"ModA,ModB,moda,  ,ModC"}}.Encode()
	reqCartForm := httptest.NewRequest("POST", "/api/valheim/wizard/cart/sync", strings.NewReader(syncForm))
	reqCartForm.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respCartForm, _ := app.Test(reqCartForm)
	bCartForm, _ := io.ReadAll(respCartForm.Body)
	if !strings.Contains(string(bCartForm), "wizard-valheim-cart-items") {
		t.Errorf("expected wizard-valheim-cart-items in sync, got: %s", string(bCartForm))
	}

	// CartSync with JSON array
	reqCartJSON := httptest.NewRequest("POST", "/api/valheim/wizard/cart/sync", strings.NewReader(`{"cart":["Mod1","Mod2"]}`))
	reqCartJSON.Header.Set("Content-Type", "application/json")
	respCartJSON, _ := app.Test(reqCartJSON)
	bCartJSON, _ := io.ReadAll(respCartJSON.Body)
	if !strings.Contains(string(bCartJSON), "wizard-valheim-cart-items") {
		t.Errorf("expected wizard-valheim-cart-items in json sync, got: %s", string(bCartJSON))
	}

	// CartSync fallback to c.FormValue("cart") comma-separated when body is empty
	reqCartFallback := httptest.NewRequest("POST", "/api/valheim/wizard/cart/sync?cart=ModX,ModY,ModX", nil)
	respCartFallback, _ := app.Test(reqCartFallback)
	bCartFallback, _ := io.ReadAll(respCartFallback.Body)
	if !strings.Contains(string(bCartFallback), "wizard-valheim-cart-items") {
		t.Errorf("expected wizard-valheim-cart-items in query sync, got: %s", string(bCartFallback))
	}

	// CartSync with empty body and empty cart
	reqCartEmpty := httptest.NewRequest("POST", "/api/valheim/wizard/cart/sync", nil)
	respCartEmpty, _ := app.Test(reqCartEmpty)
	bCartEmpty, _ := io.ReadAll(respCartEmpty.Body)
	if !strings.Contains(string(bCartEmpty), "wizard-valheim-cart-items") {
		t.Errorf("expected wizard-valheim-cart-items in empty sync, got: %s", string(bCartEmpty))
	}

	// 4. WizardModDetail edges
	// Missing slug
	respWizNoSlug, _ := app.Test(httptest.NewRequest("GET", "/api/valheim/wizard/mods/detail", nil))
	bWizNoSlug, _ := io.ReadAll(respWizNoSlug.Body)
	if !strings.Contains(string(bWizNoSlug), "Mod slug required") {
		t.Errorf("expected Mod slug required, got: %s", string(bWizNoSlug))
	}

	// TS is nil
	appUnconf.Get("/api/valheim/wizard/mods/detail", hUnconf.ValheimWizardModDetail)
	respWizNoTS, _ := appUnconf.Test(httptest.NewRequest("GET", "/api/valheim/wizard/mods/detail?slug=Custom/Mod", nil))
	bWizNoTS, _ := io.ReadAll(respWizNoTS.Body)
	if !strings.Contains(string(bWizNoTS), "Custom") {
		t.Errorf("expected Custom in drawer, got: %s", string(bWizNoTS))
	}

	// With ReadmeCache miss & hit
	readmeCache := &mockValheimReadmeCache{}
	h.cfg.ReadmeCache = readmeCache
	reqWizDetailMiss := httptest.NewRequest("GET", "/api/valheim/wizard/mods/detail?slug=Viking/Shield", nil)
	respWizDetailMiss, _ := app.Test(reqWizDetailMiss)
	bWizDetailMiss, _ := io.ReadAll(respWizDetailMiss.Body)
	if !strings.Contains(string(bWizDetailMiss), "Awesome Valheim mod") {
		t.Errorf("expected readme, got: %s", string(bWizDetailMiss))
	}

	reqWizDetailHit := httptest.NewRequest("GET", "/api/valheim/wizard/mods/detail?slug=Viking/Shield", nil)
	respWizDetailHit, _ := app.Test(reqWizDetailHit)
	bWizDetailHit, _ := io.ReadAll(respWizDetailHit.Body)
	if !strings.Contains(string(bWizDetailHit), "Awesome Valheim mod") {
		t.Errorf("expected cached readme, got: %s", string(bWizDetailHit))
	}

	// Test OnlySlug in ValheimWizardModDetail (triggers TS.Get(slug), version == "", and dependencies)
	mockTS.results = append(mockTS.results, domain.ModSearchResult{
		Owner:       "OnlySlugAuthor",
		Name:        "OnlyWizSlugMod",
		Version:     "",
		Description: "Only wiz slug mod",
	})
	reqOnlySlugWizDetail := httptest.NewRequest("GET", "/api/valheim/wizard/mods/detail?slug=OnlyWizSlugMod", nil)
	respOnlySlugWizDetail, _ := app.Test(reqOnlySlugWizDetail)
	bOnlySlugWizDetail, _ := io.ReadAll(respOnlySlugWizDetail.Body)
	if !strings.Contains(string(bOnlySlugWizDetail), "OnlySlugAuthor") || !strings.Contains(string(bOnlySlugWizDetail), "Required Dependencies") {
		t.Errorf("expected OnlySlugAuthor and Required Dependencies, got: %s", string(bOnlySlugWizDetail))
	}

	// Seam errors in ValheimWizardModDetail
	origInner := innerElement
	origPatch := patchSignals
	t.Cleanup(func() {
		innerElement = origInner
		patchSignals = origPatch
	})

	innerElement = func(w *bufio.Writer, sel, el string) error { return errors.New("inner failed") }
	respWizInnerErr, _ := app.Test(httptest.NewRequest("GET", "/api/valheim/wizard/mods/detail?slug=Viking/Shield", nil))
	if respWizInnerErr.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500, got %d", respWizInnerErr.StatusCode)
	}

	innerElement = origInner
	patchSignals = func(w *bufio.Writer, s any) error { return errors.New("patch failed") }
	respWizPatchErr, _ := app.Test(httptest.NewRequest("GET", "/api/valheim/wizard/mods/detail?slug=Viking/Shield", nil))
	if respWizPatchErr.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500, got %d", respWizPatchErr.StatusCode)
	}
	patchSignals = origPatch

	// 5. WizardImport edges
	// Corrupt zip
	var badBody bytes.Buffer
	badMpw := multipart.NewWriter(&badBody)
	part, _ := badMpw.CreateFormFile("file", "bad.r2z")
	_, _ = part.Write([]byte("not a zip file at all"))
	_ = badMpw.Close()

	reqBadZip := httptest.NewRequest("POST", "/api/valheim/wizard/import", &badBody)
	reqBadZip.Header.Set("Content-Type", badMpw.FormDataContentType())
	respBadZip, _ := app.Test(reqBadZip)
	bBadZip, _ := io.ReadAll(respBadZip.Body)
	if !strings.Contains(string(bBadZip), "Failed to parse modpack profile") {
		t.Errorf("expected parse profile failed toast, got: %s", string(bBadZip))
	}

	// Valid zip without name
	validNoNameZip := makeTestR2Z("", []string{"Smoothbrain-Mining-1.2.0"})
	var validBody bytes.Buffer
	validMpw := multipart.NewWriter(&validBody)
	part2, _ := validMpw.CreateFormFile("file", "valid.r2z")
	_, _ = part2.Write(validNoNameZip)
	_ = validMpw.Close()

	reqValidZip := httptest.NewRequest("POST", "/api/valheim/wizard/import", &validBody)
	reqValidZip.Header.Set("Content-Type", validMpw.FormDataContentType())
	respValidZip, _ := app.Test(reqValidZip)
	bValidZip, _ := io.ReadAll(respValidZip.Body)
	if !strings.Contains(string(bValidZip), "Imported 1 mods from profile!") {
		t.Errorf("expected imported 1 mods, got: %s", string(bValidZip))
	}

	// Seam errors in ValheimWizardImport
	origOpenFormFile := openFormFile
	origReadFormFile := readFormFile
	t.Cleanup(func() {
		openFormFile = origOpenFormFile
		readFormFile = origReadFormFile
	})

	// Test openFormFile error
	openFormFile = func(fh *multipart.FileHeader) (multipart.File, error) {
		return nil, errors.New("open error")
	}
	var openErrBody bytes.Buffer
	openErrMpw := multipart.NewWriter(&openErrBody)
	partOpenErr, _ := openErrMpw.CreateFormFile("file", "valid.r2z")
	_, _ = partOpenErr.Write(validNoNameZip)
	_ = openErrMpw.Close()

	reqOpenErr := httptest.NewRequest("POST", "/api/valheim/wizard/import", &openErrBody)
	reqOpenErr.Header.Set("Content-Type", openErrMpw.FormDataContentType())
	respOpenErr, _ := app.Test(reqOpenErr)
	bOpenErr, _ := io.ReadAll(respOpenErr.Body)
	if !strings.Contains(string(bOpenErr), "Failed to open uploaded file: open error") {
		t.Errorf("expected open error toast, got: %s", string(bOpenErr))
	}
	openFormFile = origOpenFormFile

	// Test readFormFile error
	readFormFile = func(r io.Reader) ([]byte, error) {
		return nil, errors.New("read error")
	}
	var readErrBody bytes.Buffer
	readErrMpw := multipart.NewWriter(&readErrBody)
	partReadErr, _ := readErrMpw.CreateFormFile("file", "valid.r2z")
	_, _ = partReadErr.Write(validNoNameZip)
	_ = readErrMpw.Close()

	reqReadErr := httptest.NewRequest("POST", "/api/valheim/wizard/import", &readErrBody)
	reqReadErr.Header.Set("Content-Type", readErrMpw.FormDataContentType())
	respReadErr, _ := app.Test(reqReadErr)
	bReadErr, _ := io.ReadAll(respReadErr.Body)
	if !strings.Contains(string(bReadErr), "Read upload failed: read error") {
		t.Errorf("expected read error toast, got: %s", string(bReadErr))
	}
	readFormFile = origReadFormFile

	// 6. WizardCreate edges
	// Unconfigured
	appUnconf.Post("/api/valheim/wizard/create", hUnconf.ValheimWizardCreate)
	respUnconfCreate, _ := appUnconf.Test(httptest.NewRequest("POST", "/api/valheim/wizard/create", nil))
	bUnconfCreate, _ := io.ReadAll(respUnconfCreate.Body)
	if !strings.Contains(string(bUnconfCreate), "unconfigured") {
		t.Errorf("expected unconfigured, got: %s", string(bUnconfCreate))
	}

	// Empty name
	reqNoName := httptest.NewRequest("POST", "/api/valheim/wizard/create", strings.NewReader(`{"name":""}`))
	reqNoName.Header.Set("Content-Type", "application/json")
	respNoName, _ := app.Test(reqNoName)
	bNoName, _ := io.ReadAll(respNoName.Body)
	if !strings.Contains(string(bNoName), "World name is required") {
		t.Errorf("expected World name is required, got: %s", string(bNoName))
	}

	// Short password
	reqShortPwd := httptest.NewRequest("POST", "/api/valheim/wizard/create", strings.NewReader(`{"name":"VikingWorld","password":"123"}`))
	reqShortPwd.Header.Set("Content-Type", "application/json")
	respShortPwd, _ := app.Test(reqShortPwd)
	bShortPwd, _ := io.ReadAll(respShortPwd.Body)
	if !strings.Contains(string(bShortPwd), "at least 5 characters") {
		t.Errorf("expected at least 5 characters, got: %s", string(bShortPwd))
	}

	// Vanilla creation (source == "vanilla" and no mods)
	reqVanilla := httptest.NewRequest("POST", "/api/valheim/wizard/create", strings.NewReader(`{"name":"VanillaWorld","source":"vanilla"}`))
	reqVanilla.Header.Set("Content-Type", "application/json")
	respVanilla, _ := app.Test(reqVanilla)
	bVanilla, _ := io.ReadAll(respVanilla.Body)
	if !strings.Contains(string(bVanilla), "Created Valheim server") {
		t.Errorf("expected Created Valheim server, got: %s", string(bVanilla))
	}

	// Form values submission with scratch, raw_mods comments, cart and mods deduplication
	formValues := url.Values{
		"name":     {"Scratch World"},
		"password": {"secretpassword"},
		"seed":     {"myseed"},
		"tier":     {"large"},
		"source":   {"scratch"},
		"raw_mods": {"# comment\nModA\n\nModB\nModA"},
	}.Encode()
	reqScratch := httptest.NewRequest("POST", "/api/valheim/wizard/create", strings.NewReader(formValues))
	reqScratch.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respScratch, _ := app.Test(reqScratch)
	bScratch, _ := io.ReadAll(respScratch.Body)
	if !strings.Contains(string(bScratch), "Created Valheim server") {
		t.Errorf("expected Created Valheim server, got: %s", string(bScratch))
	}

	// Creation with JSON mods array
	reqModsJSON := httptest.NewRequest("POST", "/api/valheim/wizard/create", strings.NewReader(`{"name":"ModsJsonWorld","source":"scratch","mods":["ModA","ModB"]}`))
	reqModsJSON.Header.Set("Content-Type", "application/json")
	respModsJSON, _ := app.Test(reqModsJSON)
	bModsJSON, _ := io.ReadAll(respModsJSON.Body)
	if !strings.Contains(string(bModsJSON), "Created Valheim server") {
		t.Errorf("expected Created Valheim server with mods json, got: %s", string(bModsJSON))
	}

	// CreateInstance error (e.g. limit reached on a 1-instance manager)
	stLimit, _ := store.Open(filepath.Join(t.TempDir(), "valheim-limit.db"))
	defer stLimit.Close()
	mgrLimit := instances.NewInstanceManager(
		store.NewValheimInstanceRepo(stLimit), newMemStateStore(), &mockRuntime{},
		16, 1, 1, "manifests/valheim", "192.168.20.224",
		valheimmanifests.New("ykhi.xyz/gameserver=true", "valheim"), "valheim",
		instances.WithGameID(domain.GameValheim),
	)
	// Create slot 1
	_, _ = mgrLimit.CreateInstance(context.Background(), domain.Instance{
		GameID: domain.GameValheim,
		Number: 1,
		Name:   "Single Instance",
		Tier:   domain.TierMedium,
	}, "", "tester")

	hLimit := New(Config{ValheimInstances: mgrLimit})
	appLimit := fiber.New()
	appLimit.Post("/api/valheim/wizard/create", hLimit.ValheimWizardCreate)

	reqOverLimit := httptest.NewRequest("POST", "/api/valheim/wizard/create", strings.NewReader(`{"name":"OverLimit"}`))
	reqOverLimit.Header.Set("Content-Type", "application/json")
	respOverLimit, _ := appLimit.Test(reqOverLimit)
	bOverLimit, _ := io.ReadAll(respOverLimit.Body)
	if !strings.Contains(string(bOverLimit), "Failed to create Valheim server") {
		t.Errorf("expected Failed to create Valheim server, got: %s", string(bOverLimit))
	}
}

func TestValheimUpdatesAllEdges(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "valheim-upd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mockState := newMemStateStore()
	rt := &mockRuntime{status: ports.Status{Lifecycle: ports.LifecycleRunning, Available: true}}
	bDir := t.TempDir()

	var currentMods []string
	var modsReaderErr error
	mgr := instances.NewInstanceManager(
		store.NewValheimInstanceRepo(st), mockState, rt,
		16, 4, 4, "manifests/valheim", "192.168.20.224",
		valheimmanifests.New("ykhi.xyz/gameserver=true", "valheim"), "valheim",
		instances.WithGameID(domain.GameValheim),
		instances.WithBackupsDir(bDir),
		instances.WithModsReader(func(ctx context.Context, num int) ([]string, error) {
			if modsReaderErr != nil {
				return nil, modsReaderErr
			}
			return currentMods, nil
		}),
	)

	// Create modded instance (num 1)
	inst, err := mgr.CreateInstance(context.Background(), domain.Instance{
		GameID: domain.GameValheim,
		Number: 1,
		Name:   "Viking Realm",
		Tier:   domain.TierMedium,
		State:  domain.StateRunning,
		Source: domain.SourceModlist,
	}, "Author-Mod-1.0.0\n", "tester")
	if err != nil {
		t.Fatal(err)
	}
	currentMods = []string{"Author-Mod-1.0.0"}

	// Create vanilla instance (num 2)
	_, err = mgr.CreateInstance(context.Background(), domain.Instance{
		GameID: domain.GameValheim,
		Number: 2,
		Name:   "Vanilla Realm",
		Tier:   domain.TierSmall,
		State:  domain.StateStopped,
		Source: domain.SourceVanilla,
	}, "", "tester")
	if err != nil {
		t.Fatal(err)
	}

	advCatalog := &mockAdvValheimCatalog{
		latestMap: map[string]struct {
			ver  string
			deps []string
			err  error
		}{
			"Author/Mod": {ver: "2.0.0", deps: nil, err: nil},
		},
	}
	checker := modupdates.New(mgr, advCatalog, modupdates.WithGameID(domain.GameValheim), modupdates.WithRestorePoints(st))
	h := New(Config{ValheimInstances: mgr, ModUpdates: checker})

	app := fiber.New()
	app.Post("/api/valheim/:num/mods/updates/check", h.ValheimModUpdatesCheck)
	app.Post("/api/valheim/:num/mods/updates/apply", h.ValheimModUpdatesApply)
	app.Post("/api/valheim/:num/mods/updates/undo", h.ValheimModUpdatesUndo)

	// 1. ModUpdateState edges
	// Unconfigured
	hUnconf := New(Config{})
	dUnconf := pages.InstanceDetailUI{}
	hUnconf.ModUpdateState(context.Background(), &dUnconf, *inst)
	if dUnconf.UpdatesChecked != "" {
		t.Errorf("expected empty UpdatesChecked when unconfigured")
	}

	// With restore point available
	_ = st.SaveRestorePoint(domain.GameValheim, 1, []string{"Author-Mod-0.9.0"}, []string{"Author-Mod-1.0.0"})
	dWithRestore := pages.InstanceDetailUI{}
	h.ModUpdateState(context.Background(), &dWithRestore, *inst)
	if !dWithRestore.CanUndo {
		t.Errorf("expected CanUndo to be true when restore point matches")
	}

	// With report error: trigger by setting modsReaderErr
	modsReaderErr = errors.New("read installed mods failed")
	_, _ = app.Test(httptest.NewRequest("POST", "/api/valheim/1/mods/updates/check", nil))
	dWithErr := pages.InstanceDetailUI{}
	h.ModUpdateState(context.Background(), &dWithErr, *inst)
	if !strings.Contains(dWithErr.UpdatesError, "Last check failed") {
		t.Errorf("expected UpdatesError to contain Last check failed, got: %s", dWithErr.UpdatesError)
	}
	modsReaderErr = nil

	// 2. ValheimModUpdatesCheck edges
	// UpdatableInstance error cases:
	// - unconfigured manager
	appUnconf := fiber.New()
	appUnconf.Post("/api/valheim/:num/mods/updates/check", hUnconf.ValheimModUpdatesCheck)
	appUnconf.Post("/api/valheim/:num/mods/updates/apply", hUnconf.ValheimModUpdatesApply)
	appUnconf.Post("/api/valheim/:num/mods/updates/undo", hUnconf.ValheimModUpdatesUndo)

	respUnconfCheck, _ := appUnconf.Test(httptest.NewRequest("POST", "/api/valheim/1/mods/updates/check", nil))
	bUnconfCheck, _ := io.ReadAll(respUnconfCheck.Body)
	if !strings.Contains(string(bUnconfCheck), "unconfigured") {
		t.Errorf("expected unconfigured, got: %s", string(bUnconfCheck))
	}

	// - bad num
	respBadNumCheck, _ := app.Test(httptest.NewRequest("POST", "/api/valheim/bad/mods/updates/check", nil))
	bBadNumCheck, _ := io.ReadAll(respBadNumCheck.Body)
	if !strings.Contains(string(bBadNumCheck), "invalid instance number") {
		t.Errorf("expected invalid instance number, got: %s", string(bBadNumCheck))
	}

	// - not found
	respNotFoundCheck, _ := app.Test(httptest.NewRequest("POST", "/api/valheim/99/mods/updates/check", nil))
	bNotFoundCheck, _ := io.ReadAll(respNotFoundCheck.Body)
	if !strings.Contains(string(bNotFoundCheck), "not found") {
		t.Errorf("expected not found, got: %s", string(bNotFoundCheck))
	}

	// - vanilla instance
	respVanillaCheck, _ := app.Test(httptest.NewRequest("POST", "/api/valheim/2/mods/updates/check", nil))
	bVanillaCheck, _ := io.ReadAll(respVanillaCheck.Body)
	if !strings.Contains(string(bVanillaCheck), "vanilla") {
		t.Errorf("expected vanilla error, got: %s", string(bVanillaCheck))
	}

	// ModUpdates is nil
	hNoChecker := New(Config{ValheimInstances: mgr})
	appNoChecker := fiber.New()
	appNoChecker.Post("/api/valheim/:num/mods/updates/check", hNoChecker.ValheimModUpdatesCheck)
	appNoChecker.Post("/api/valheim/:num/mods/updates/apply", hNoChecker.ValheimModUpdatesApply)
	appNoChecker.Post("/api/valheim/:num/mods/updates/undo", hNoChecker.ValheimModUpdatesUndo)

	respNoCheckerCheck, _ := appNoChecker.Test(httptest.NewRequest("POST", "/api/valheim/1/mods/updates/check", nil))
	bNoCheckerCheck, _ := io.ReadAll(respNoCheckerCheck.Body)
	if !strings.Contains(string(bNoCheckerCheck), "not configured") {
		t.Errorf("expected not configured, got: %s", string(bNoCheckerCheck))
	}

	// RefreshOne report branches:
	// - updates > 0
	advCatalog.latestMap["Author/Mod"] = struct {
		ver  string
		deps []string
		err  error
	}{ver: "2.0.0", deps: nil, err: nil}
	respUpdAvail, _ := app.Test(httptest.NewRequest("POST", "/api/valheim/1/mods/updates/check", nil))
	bUpdAvail, _ := io.ReadAll(respUpdAvail.Body)
	if !strings.Contains(string(bUpdAvail), "1 mod update(s) available") {
		t.Errorf("expected 1 mod update(s) available, got: %s", string(bUpdAvail))
	}

	// - unreachable > 0
	advCatalog.latestMap["Author/Mod"] = struct {
		ver  string
		deps []string
		err  error
	}{ver: "", deps: nil, err: errors.New("upstream 503")}
	respUnreachable, _ := app.Test(httptest.NewRequest("POST", "/api/valheim/1/mods/updates/check", nil))
	bUnreachable, _ := io.ReadAll(respUnreachable.Body)
	if !strings.Contains(string(bUnreachable), "could not be checked") {
		t.Errorf("expected could not be checked, got: %s", string(bUnreachable))
	}

	// - missing > 0
	advCatalog.latestMap["Author/Mod"] = struct {
		ver  string
		deps []string
		err  error
	}{ver: "", deps: nil, err: ports.ErrPackageNotFound}
	respMissing, _ := app.Test(httptest.NewRequest("POST", "/api/valheim/1/mods/updates/check", nil))
	bMissing, _ := io.ReadAll(respMissing.Body)
	if !strings.Contains(string(bMissing), "gone from Thunderstore") {
		t.Errorf("expected gone from Thunderstore, got: %s", string(bMissing))
	}

	// - no updates available (latest equals current)
	advCatalog.latestMap["Author/Mod"] = struct {
		ver  string
		deps []string
		err  error
	}{ver: "1.0.0", deps: nil, err: nil}
	respNoUpd, _ := app.Test(httptest.NewRequest("POST", "/api/valheim/1/mods/updates/check", nil))
	bNoUpd, _ := io.ReadAll(respNoUpd.Body)
	if !strings.Contains(string(bNoUpd), "No mod updates available") {
		t.Errorf("expected No mod updates available, got: %s", string(bNoUpd))
	}

	// 3. ValheimModUpdatesApply edges
	// Updatable error & unconfigured checker
	respUnconfApply, _ := appUnconf.Test(httptest.NewRequest("POST", "/api/valheim/1/mods/updates/apply", nil))
	bUnconfApply, _ := io.ReadAll(respUnconfApply.Body)
	if !strings.Contains(string(bUnconfApply), "unconfigured") {
		t.Errorf("expected unconfigured, got: %s", string(bUnconfApply))
	}

	respNoCheckerApply, _ := appNoChecker.Test(httptest.NewRequest("POST", "/api/valheim/1/mods/updates/apply", nil))
	bNoCheckerApply, _ := io.ReadAll(respNoCheckerApply.Body)
	if !strings.Contains(string(bNoCheckerApply), "not configured") {
		t.Errorf("expected not configured, got: %s", string(bNoCheckerApply))
	}

	// body.All == false with selected keys
	advCatalog.latestMap["Author/Mod"] = struct {
		ver  string
		deps []string
		err  error
	}{ver: "2.0.0", deps: nil, err: nil}
	// Run check so updates list is cached
	_, _ = h.cfg.ModUpdates.RefreshOne(context.Background(), 1)

	selToken := pages.UpdateToken(domain.ModRef{Namespace: "Author", Name: "Mod"}.Key())
	bodyJSON := fmt.Sprintf(`{"all":false,"selected":{"%s":true}}`, selToken)
	reqApplySel := httptest.NewRequest("POST", "/api/valheim/1/mods/updates/apply", strings.NewReader(bodyJSON))
	reqApplySel.Header.Set("Content-Type", "application/json")
	respApplySel, _ := app.Test(reqApplySel)
	bApplySel, _ := io.ReadAll(respApplySel.Body)
	if !strings.Contains(string(bApplySel), "Updating 1 mod(s)") {
		t.Errorf("expected Updating 1 mod(s), got: %s", string(bApplySel))
	}

	// Consecutive Apply -> pending branch (currentMods has not synced to 2.0.0 yet)
	reqApplyPending := httptest.NewRequest("POST", "/api/valheim/1/mods/updates/apply", strings.NewReader(`{"all":true}`))
	reqApplyPending.Header.Set("Content-Type", "application/json")
	respApplyPending, _ := app.Test(reqApplyPending)
	bApplyPending, _ := io.ReadAll(respApplyPending.Body)
	if !strings.Contains(string(bApplyPending), "already in progress") {
		t.Errorf("expected already in progress, got: %s", string(bApplyPending))
	}

	// Now sync currentMods to 2.0.0 so pending clears
	currentMods = []string{"Author/Mod/2.0.0"}

	// Apply again with all=true -> len(applied) == 0 ("Already up to date")
	reqApplyUpToDate := httptest.NewRequest("POST", "/api/valheim/1/mods/updates/apply", strings.NewReader(`{"all":true}`))
	reqApplyUpToDate.Header.Set("Content-Type", "application/json")
	respApplyUpToDate, _ := app.Test(reqApplyUpToDate)
	bApplyUpToDate, _ := io.ReadAll(respApplyUpToDate.Body)
	if !strings.Contains(string(bApplyUpToDate), "Already up to date") {
		t.Errorf("expected Already up to date, got: %s", string(bApplyUpToDate))
	}

	// Apply error (when stateStore.patchErr is set)
	advCatalog.latestMap["Author/Mod"] = struct {
		ver  string
		deps []string
		err  error
	}{ver: "3.0.0", deps: nil, err: nil}
	_, _ = h.cfg.ModUpdates.RefreshOne(context.Background(), 1)
	mockState.patchErr = errors.New("git patch failure")
	reqApplyErr := httptest.NewRequest("POST", "/api/valheim/1/mods/updates/apply", strings.NewReader(`{"all":true}`))
	reqApplyErr.Header.Set("Content-Type", "application/json")
	respApplyErr, _ := app.Test(reqApplyErr)
	bApplyErr, _ := io.ReadAll(respApplyErr.Body)
	if !strings.Contains(string(bApplyErr), "Update failed: git patch failure") {
		t.Errorf("expected Update failed toast, got: %s", string(bApplyErr))
	}
	mockState.patchErr = nil

	// 4. ValheimModUpdatesUndo edges
	respUnconfUndo, _ := appUnconf.Test(httptest.NewRequest("POST", "/api/valheim/1/mods/updates/undo", nil))
	bUnconfUndo, _ := io.ReadAll(respUnconfUndo.Body)
	if !strings.Contains(string(bUnconfUndo), "unconfigured") {
		t.Errorf("expected unconfigured, got: %s", string(bUnconfUndo))
	}

	respNoCheckerUndo, _ := appNoChecker.Test(httptest.NewRequest("POST", "/api/valheim/1/mods/updates/undo", nil))
	bNoCheckerUndo, _ := io.ReadAll(respNoCheckerUndo.Body)
	if !strings.Contains(string(bNoCheckerUndo), "not configured") {
		t.Errorf("expected not configured, got: %s", string(bNoCheckerUndo))
	}

	// Undo error (e.g. no restore point exists)
	_ = st.ClearRestorePoint(domain.GameValheim, 1)
	respUndoErr, _ := app.Test(httptest.NewRequest("POST", "/api/valheim/1/mods/updates/undo", nil))
	bUndoErr, _ := io.ReadAll(respUndoErr.Body)
	if !strings.Contains(string(bUndoErr), "Undo failed") {
		t.Errorf("expected Undo failed, got: %s", string(bUndoErr))
	}

	// 5. pushUpdatePanel seam errors
	origRender := renderUpdatePanel
	origInner := innerElement
	origPatch := patchSignals
	t.Cleanup(func() {
		renderUpdatePanel = origRender
		innerElement = origInner
		patchSignals = origPatch
	})

	renderUpdatePanel = func(ctx context.Context, d pages.InstanceDetailUI, w io.Writer) error {
		return errors.New("render update panel failed")
	}
	respRenderErr, _ := app.Test(httptest.NewRequest("POST", "/api/valheim/1/mods/updates/check", nil))
	bRenderErr, _ := io.ReadAll(respRenderErr.Body)
	if !strings.Contains(string(bRenderErr), "Render failed: render update panel failed") {
		t.Errorf("expected Render failed toast, got: %s", string(bRenderErr))
	}

	renderUpdatePanel = origRender
	innerElement = func(w *bufio.Writer, sel, el string) error {
		return errors.New("innerElement failed")
	}
	respInnerErr, _ := app.Test(httptest.NewRequest("POST", "/api/valheim/1/mods/updates/check", nil))
	if respInnerErr.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500, got %d", respInnerErr.StatusCode)
	}

	innerElement = origInner
	patchSignals = func(w *bufio.Writer, s any) error {
		return errors.New("patchSignals failed")
	}
	respPatchErr, _ := app.Test(httptest.NewRequest("POST", "/api/valheim/1/mods/updates/check", nil))
	if respPatchErr.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500, got %d", respPatchErr.StatusCode)
	}
	patchSignals = origPatch
}
