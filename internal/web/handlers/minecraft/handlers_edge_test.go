package minecraft

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"agrelha/internal/app/instances"
	"agrelha/internal/app/modupdates"
	"agrelha/internal/domain"
	"agrelha/internal/infra/manifests"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
	"agrelha/internal/web/pages"
)

type mockExportGame struct {
	exportErr error
	bundle    domain.Bundle
}

func (m *mockExportGame) ID() domain.GameID { return domain.GameMinecraft }
func (m *mockExportGame) Display() domain.Display {
	return domain.Display{Name: "Minecraft"}
}
func (m *mockExportGame) Providers() []ports.ContentProvider { return nil }
func (m *mockExportGame) ResolveContent(_ context.Context, _ domain.Instance) (domain.ContentSet, error) {
	return domain.ContentSet{}, nil
}
func (m *mockExportGame) ExportClientBundle(_ context.Context, _ domain.Instance) (domain.Bundle, error) {
	if m.exportErr != nil {
		return domain.Bundle{}, m.exportErr
	}
	return m.bundle, nil
}
func (m *mockExportGame) RuntimeSpec(_ domain.Instance) domain.RuntimeSpec {
	return domain.RuntimeSpec{}
}
func (m *mockExportGame) Telemetry(_ context.Context) (domain.GameTelemetry, error) {
	return domain.GameTelemetry{}, nil
}
func (m *mockExportGame) AdmissionModel() domain.AdmissionModel { return "" }
func (m *mockExportGame) OperatorIDKind() domain.OperatorIDKind { return "" }

func TestHandlerActorAndStatsEdges(t *testing.T) {
	app := fiber.New()
	h := New(Config{})
	app.Get("/actor-default", func(c *fiber.Ctx) error {
		return c.SendString(h.cfg.Actor(c))
	})
	app.Get("/actor-custom", func(c *fiber.Ctx) error {
		c.Locals("actor", "superadmin")
		return c.SendString(h.cfg.Actor(c))
	})
	app.Get("/actor-empty", func(c *fiber.Ctx) error {
		c.Locals("actor", "")
		return c.SendString(h.cfg.Actor(c))
	})

	req1 := httptest.NewRequest("GET", "/actor-default", nil)
	resp1, _ := app.Test(req1)
	b1, _ := io.ReadAll(resp1.Body)
	if string(b1) != "local" {
		t.Errorf("expected local, got %s", b1)
	}

	req2 := httptest.NewRequest("GET", "/actor-custom", nil)
	resp2, _ := app.Test(req2)
	b2, _ := io.ReadAll(resp2.Body)
	if string(b2) != "superadmin" {
		t.Errorf("expected superadmin, got %s", b2)
	}

	req3 := httptest.NewRequest("GET", "/actor-empty", nil)
	resp3, _ := app.Test(req3)
	b3, _ := io.ReadAll(resp3.Body)
	if string(b3) != "local" {
		t.Errorf("expected local, got %s", b3)
	}

	// Default ApplyMinecraftAfterSync no-op
	h.cfg.ApplyMinecraftAfterSync("cm", "dep", "key", func(string) bool { return true })

	// LegacyModsRedirect and LegacyConfigsRedirect when MCInstances is nil
	appRoutes := fiber.New()
	h.RegisterProtected(appRoutes)

	respModsNil, _ := appRoutes.Test(httptest.NewRequest("GET", "/minecraft/mods", nil))
	if respModsNil.StatusCode != fiber.StatusTemporaryRedirect {
		t.Errorf("expected 307 for nil mods redirect, got: %d", respModsNil.StatusCode)
	}

	respCfgNil, _ := appRoutes.Test(httptest.NewRequest("GET", "/minecraft/configs", nil))
	if respCfgNil.StatusCode != fiber.StatusTemporaryRedirect {
		t.Errorf("expected 307 for nil configs redirect, got: %d", respCfgNil.StatusCode)
	}

	// InstanceStats when MCInstances is nil
	if res := h.InstanceStats(context.Background(), nil); res != nil {
		t.Errorf("expected nil stats for nil MCInstances, got: %+v", res)
	}

	// With initialized instances handler
	hFull, st, _, mgr := setupTestInstancesHandler(t)
	defer st.Close()

	// Empty list redirect
	appFull := fiber.New()
	hFull.RegisterProtected(appFull)
	respCfgEmpty, _ := appFull.Test(httptest.NewRequest("GET", "/minecraft/configs", nil))
	if respCfgEmpty.StatusCode != fiber.StatusTemporaryRedirect {
		t.Errorf("expected 307 for empty configs redirect, got: %d", respCfgEmpty.StatusCode)
	}

	respModsEmpty, _ := appFull.Test(httptest.NewRequest("GET", "/minecraft/mods", nil))
	if respModsEmpty.StatusCode != fiber.StatusTemporaryRedirect {
		t.Errorf("expected 307 for empty mods redirect, got: %d", respModsEmpty.StatusCode)
	}

	// With instance created
	_, _ = mgr.CreateInstance(context.Background(), domain.Instance{
		Number:    1,
		Name:      "World1",
		State:     domain.StateRunning,
		MCVersion: "1.21.1",
	}, "")

	respCfgInst, _ := appFull.Test(httptest.NewRequest("GET", "/minecraft/configs", nil))
	if loc := respCfgInst.Header.Get("Location"); loc != "/minecraft/1/configs" {
		t.Errorf("expected redirect to /minecraft/1/configs, got: %s", loc)
	}

	// InstanceStats caching test: call twice within 15s TTL
	insts := []domain.Instance{{Number: 1, State: domain.StateRunning}}
	stats1 := hFull.InstanceStats(context.Background(), insts)
	stats2 := hFull.InstanceStats(context.Background(), insts)
	if stats1 == nil || stats2 == nil {
		t.Errorf("expected non-nil stats, got: %+v, %+v", stats1, stats2)
	}
}

func TestMCDashboardAllBranches(t *testing.T) {
	h, st, _, mgr := setupTestInstancesHandler(t)
	defer st.Close()

	// Create instances with various states and properties
	// 1. Running instance
	_, _ = mgr.CreateInstance(context.Background(), domain.Instance{
		Number:    1,
		Name:      "RunningWorld",
		State:     domain.StateRunning,
		Tier:      domain.TierMedium,
		MCVersion: "1.21.1",
	}, "")

	// 2. Modpack instance
	_, _ = mgr.CreateInstance(context.Background(), domain.Instance{
		Number:    2,
		Name:      "ModpackWorld",
		State:     domain.StateStopped,
		Tier:      domain.TierSmall,
		MCVersion: "1.21.1",
		Source:    domain.SourceModpack,
		Pack: &domain.Pack{
			Name:     "AllTheMods",
			Ref:      "atm-9",
			Provider: domain.ProviderModrinth,
		},
	}, "")

	// 3. Stopped instance that would exceed RAM if started
	_, _ = mgr.CreateInstance(context.Background(), domain.Instance{
		Number:    3,
		Name:      "LargeWorld",
		State:     domain.StateStopped,
		Tier:      domain.TierLarge,
		MCVersion: "1.21.1",
	}, "")

	cat := &mockModUpdatesCatalog{
		versions: map[string]string{"jei|fabric": "15.0.0"},
	}
	h.cfg.ModUpdates = modupdates.New(mgr, cat, modupdates.WithGameID(domain.GameMinecraft), modupdates.WithRestorePoints(st))

	app := fiber.New()
	h.RegisterProtected(app)

	// Verify dashboard renders with all instances, pack info, and update count
	req := httptest.NewRequest("GET", "/minecraft", nil)
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 OK for dashboard, got: %v, code: %d", err, resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "AllTheMods") || !strings.Contains(string(body), "RunningWorld") {
		t.Errorf("expected dashboard content to contain instance names, got: %s", string(body))
	}

	// Close store and verify 500 error on ListInstances failure
	_ = st.Close()
	reqErr := httptest.NewRequest("GET", "/minecraft", nil)
	respErr, _ := app.Test(reqErr)
	if respErr.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500 for closed db in dashboard, got: %d", respErr.StatusCode)
	}
}

func TestMCInstanceCreateStartStopRestartDeleteEdges(t *testing.T) {
	h, st, _, mgr := setupTestInstancesHandler(t)
	defer st.Close()

	app := fiber.New()
	h.RegisterProtected(app)

	// 1. Create with custom loader (not vanilla)
	formModlist := url.Values{
		"name":       {"FabricWorld"},
		"loader":     {"fabric"},
		"tier":       {"small"},
		"mc_version": {""}, // empty -> fallback to 1.21.1
		"seed":       {"12345"},
		"mods":       {"jei\n"},
	}
	reqCreate := httptest.NewRequest("POST", "/api/minecraft/instances", strings.NewReader(formModlist.Encode()))
	reqCreate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respCreate, err := app.Test(reqCreate)
	if err != nil || respCreate.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for create fabric world, got: %v, code: %d", err, respCreate.StatusCode)
	}

	// 2. Start already running instance
	reqStart := httptest.NewRequest("POST", "/api/minecraft/instances/1/start", nil)
	respStart, _ := app.Test(reqStart)
	if respStart.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for start, got: %d", respStart.StatusCode)
	}

	// 3. Stop running instance
	reqStop := httptest.NewRequest("POST", "/api/minecraft/instances/1/stop", nil)
	respStop, _ := app.Test(reqStop)
	if respStop.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for stop, got: %d", respStop.StatusCode)
	}

	// 4. Restart instance
	reqRestart := httptest.NewRequest("POST", "/api/minecraft/instances/1/restart", nil)
	respRestart, _ := app.Test(reqRestart)
	if respRestart.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for restart, got: %d", respRestart.StatusCode)
	}

	// 5. Delete instance
	reqDelete := httptest.NewRequest("DELETE", "/api/minecraft/instances/1", nil)
	respDelete, _ := app.Test(reqDelete)
	if respDelete.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for delete, got: %d", respDelete.StatusCode)
	}

	// 6. Non-existent instance operations return error toasts
	for _, ep := range []struct {
		method string
		path   string
		expect string
	}{
		{"POST", "/api/minecraft/instances/99/start", "Failed to start world"},
		{"POST", "/api/minecraft/instances/99/stop", "Failed to stop world"},
		{"POST", "/api/minecraft/instances/99/restart", "Restart failed"},
		{"DELETE", "/api/minecraft/instances/99", "Failed to delete world"},
	} {
		r := httptest.NewRequest(ep.method, ep.path, nil)
		resp, _ := app.Test(r)
		b, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(b), ep.expect) {
			t.Errorf("%s %s expected %s, got: %s", ep.method, ep.path, ep.expect, string(b))
		}
	}

	// 7. CreateInstance failure on closed db
	_ = st.Close()
	reqFail := httptest.NewRequest("POST", "/api/minecraft/instances", strings.NewReader("name=FailWorld&loader=vanilla"))
	reqFail.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respFail, _ := app.Test(reqFail)
	bFail, _ := io.ReadAll(respFail.Body)
	if !strings.Contains(string(bFail), "Failed to create world") {
		t.Errorf("expected Failed to create world toast, got: %s", string(bFail))
	}
	_ = mgr
}

func TestMCInstanceDetailAllBranches(t *testing.T) {
	h, st, _, mgr, mockState := setupTestInstancesHandlerWithState(t)
	defer st.Close()

	// Seed instance with Pack
	inst, _ := mgr.CreateInstance(context.Background(), domain.Instance{
		Number:    1,
		Name:      "DetailWorld",
		State:     domain.StateRunning,
		Tier:      domain.TierSmall,
		MCVersion: "1.21.1",
		Source:    domain.SourceModpack,
		Pack: &domain.Pack{
			Name:     "CobblemonPack",
			Ref:      "cobblemon",
			Provider: domain.ProviderCurseForge,
		},
	}, "")

	// Set LastIncident hook
	var incidentErr error
	h.cfg.LastIncident = func(ctx context.Context, number int) (*domain.Incident, error) {
		if incidentErr != nil {
			return nil, incidentErr
		}
		return &domain.Incident{
			ID:     1,
			Reason: "Crash on boot",
		}, nil
	}

	app := fiber.New()
	h.Register(app)

	// Register unconstrained routes to test strconv.Atoi error paths
	appBad := fiber.New()
	appBad.Get("/test/:num/:tab", h.MCInstancePage)
	appBad.Post("/test/:num/settings", h.MCInstanceSettingsSave)
	appBad.Post("/test/:num/mods/remove", h.MCInstanceModsRemove)
	appBad.Post("/test/:num/mods/install", h.MCInstanceModsInstall)
	appBad.Get("/test/:num/export", h.MCInstanceExport)

	// 1. Redirect /minecraft/:num to /minecraft/:num/overview
	reqRedir := httptest.NewRequest("GET", "/minecraft/1", nil)
	respRedir, _ := app.Test(reqRedir)
	if respRedir.StatusCode != fiber.StatusFound {
		t.Errorf("expected 302 redirect for instance root, got: %d", respRedir.StatusCode)
	}

	// 2. MCInstancePage overview tab (default)
	reqOverview := httptest.NewRequest("GET", "/minecraft/1/overview", nil)
	respOverview, err := app.Test(reqOverview)
	if err != nil || respOverview.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for overview tab, got: %v, code: %d", err, respOverview.StatusCode)
	}

	// 3. MCInstancePage empty tab fallback to overview
	reqEmptyTab := httptest.NewRequest("GET", "/minecraft/1/", nil)
	respEmptyTab, _ := app.Test(reqEmptyTab)
	if respEmptyTab.StatusCode != fiber.StatusOK && respEmptyTab.StatusCode != fiber.StatusFound {
		t.Errorf("unexpected status for empty tab: %d", respEmptyTab.StatusCode)
	}

	// 4. MCInstancePage backups tab
	reqBackups := httptest.NewRequest("GET", "/minecraft/1/backups", nil)
	respBackups, _ := app.Test(reqBackups)
	if respBackups.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for backups tab, got: %d", respBackups.StatusCode)
	}

	// 5. MCInstancePage with LastIncident returning error
	incidentErr = errors.New("incident lookup failed")
	reqIncErr := httptest.NewRequest("GET", "/minecraft/1/overview", nil)
	respIncErr, _ := app.Test(reqIncErr)
	if respIncErr.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 even with incident lookup error, got: %d", respIncErr.StatusCode)
	}

	// 6. MCInstancePage invalid number & not found & nil manager
	appNil := fiber.New()
	hNil := New(Config{})
	hNil.Register(appNil)
	respNilPage, _ := appNil.Test(httptest.NewRequest("GET", "/minecraft/1/overview", nil))
	if respNilPage.StatusCode != fiber.StatusServiceUnavailable {
		t.Errorf("expected 503 for nil MCInstances, got: %d", respNilPage.StatusCode)
	}

	respBadNum, _ := appBad.Test(httptest.NewRequest("GET", "/test/invalid/overview", nil))
	if respBadNum.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for bad instance num, got: %d", respBadNum.StatusCode)
	}

	respNotFound, _ := app.Test(httptest.NewRequest("GET", "/minecraft/999/overview", nil))
	if respNotFound.StatusCode != fiber.StatusNotFound {
		t.Errorf("expected 404 for missing instance, got: %d", respNotFound.StatusCode)
	}

	// 7. MCInstanceSettingsSave with form values & errors
	respNilSave, _ := appNil.Test(httptest.NewRequest("POST", "/api/minecraft/1/settings", nil))
	bNilSave, _ := io.ReadAll(respNilSave.Body)
	if !strings.Contains(string(bNilSave), "Instance manager unconfigured") {
		t.Errorf("expected unconfigured error, got: %s", bNilSave)
	}

	respBadSaveNum, _ := appBad.Test(httptest.NewRequest("POST", "/test/invalid/settings", nil))
	bBadSaveNum, _ := io.ReadAll(respBadSaveNum.Body)
	if !strings.Contains(string(bBadSaveNum), "Invalid instance number") {
		t.Errorf("expected invalid instance number, got: %s", bBadSaveNum)
	}

	// Form values saving
	formSettings := url.Values{
		"name":       {"NewWorldName"},
		"motd":       {"New MOTD"},
		"tier":       {"medium"},
		"mc_version": {"1.21.1"},
	}
	reqFormSave := httptest.NewRequest("POST", "/api/minecraft/1/settings", strings.NewReader(formSettings.Encode()))
	reqFormSave.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respFormSave, _ := app.Test(reqFormSave)
	bFormSave, _ := io.ReadAll(respFormSave.Body)
	if !strings.Contains(string(bFormSave), "Settings saved successfully") {
		t.Errorf("expected settings saved toast, got: %s", bFormSave)
	}

	// Settings save error for non-existent instance
	reqErrSave := httptest.NewRequest("POST", "/api/minecraft/999/settings", strings.NewReader(`{"name":"X"}`))
	reqErrSave.Header.Set("Content-Type", "application/json")
	respErrSave, _ := app.Test(reqErrSave)
	bErrSave, _ := io.ReadAll(respErrSave.Body)
	if !strings.Contains(string(bErrSave), "Failed to update settings") {
		t.Errorf("expected failed to update settings toast, got: %s", bErrSave)
	}

	// 8. MCInstanceModsRemove & Install fallbacks and error paths
	respNilRemove, _ := appNil.Test(httptest.NewRequest("POST", "/api/minecraft/1/mods/remove", nil))
	bNilRemove, _ := io.ReadAll(respNilRemove.Body)
	if !strings.Contains(string(bNilRemove), "Instance manager unconfigured") {
		t.Errorf("expected unconfigured, got: %s", bNilRemove)
	}

	respNilInstall, _ := appNil.Test(httptest.NewRequest("POST", "/api/minecraft/1/mods/install", nil))
	bNilInstall, _ := io.ReadAll(respNilInstall.Body)
	if !strings.Contains(string(bNilInstall), "Instance manager unconfigured") {
		t.Errorf("expected unconfigured, got: %s", bNilInstall)
	}

	respBadRemoveNum, _ := appBad.Test(httptest.NewRequest("POST", "/test/invalid/mods/remove", nil))
	bBadRemoveNum, _ := io.ReadAll(respBadRemoveNum.Body)
	if !strings.Contains(string(bBadRemoveNum), "Invalid instance number") {
		t.Errorf("expected invalid instance number, got: %s", bBadRemoveNum)
	}

	respBadInstallNum, _ := appBad.Test(httptest.NewRequest("POST", "/test/invalid/mods/install", nil))
	bBadInstallNum, _ := io.ReadAll(respBadInstallNum.Body)
	if !strings.Contains(string(bBadInstallNum), "Invalid instance number") {
		t.Errorf("expected invalid instance number, got: %s", bBadInstallNum)
	}

	// Slug empty checks
	respEmptyRemove, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/1/mods/remove", strings.NewReader(`{"slug":""}`)))
	bEmptyRemove, _ := io.ReadAll(respEmptyRemove.Body)
	if !strings.Contains(string(bEmptyRemove), "Mod slug required") {
		t.Errorf("expected mod slug required, got: %s", bEmptyRemove)
	}

	respEmptyInstall, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/1/mods/install", strings.NewReader(`{"slug":""}`)))
	bEmptyInstall, _ := io.ReadAll(respEmptyInstall.Body)
	if !strings.Contains(string(bEmptyInstall), "Mod slug required") {
		t.Errorf("expected mod slug required, got: %s", bEmptyInstall)
	}

	// Slug from FormValue and Query
	reqFormRemove := httptest.NewRequest("POST", "/api/minecraft/1/mods/remove", strings.NewReader("slug=jei"))
	reqFormRemove.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respFormRemove, _ := app.Test(reqFormRemove)
	if respFormRemove.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for form mod remove, got: %d", respFormRemove.StatusCode)
	}

	reqQueryRemove := httptest.NewRequest("POST", "/api/minecraft/1/mods/remove?slug=ferrite-core", nil)
	respQueryRemove, _ := app.Test(reqQueryRemove)
	if respQueryRemove.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for query mod remove, got: %d", respQueryRemove.StatusCode)
	}

	reqFormInstall := httptest.NewRequest("POST", "/api/minecraft/1/mods/install", strings.NewReader("slug=appleskin"))
	reqFormInstall.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respFormInstall, _ := app.Test(reqFormInstall)
	if respFormInstall.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for form mod install, got: %d", respFormInstall.StatusCode)
	}

	reqQueryInstall := httptest.NewRequest("POST", "/api/minecraft/1/mods/install?slug=cloth-config", nil)
	respQueryInstall, _ := app.Test(reqQueryInstall)
	if respQueryInstall.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for query mod install, got: %d", respQueryInstall.StatusCode)
	}

	// Errors via mockState.patchErr
	mockState.patchErr = errors.New("simulated patch failure")
	respErrRemove, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/1/mods/remove?slug=jei", nil))
	bErrRemove, _ := io.ReadAll(respErrRemove.Body)
	if !strings.Contains(string(bErrRemove), "Remove failed") {
		t.Errorf("expected Remove failed toast, got: %s", bErrRemove)
	}

	respErrInstall, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/1/mods/install?slug=jei", nil))
	bErrInstall, _ := io.ReadAll(respErrInstall.Body)
	if !strings.Contains(string(bErrInstall), "Install failed") {
		t.Errorf("expected Install failed toast, got: %s", bErrInstall)
	}
	mockState.patchErr = nil

	// 9. MCInstanceExport tests
	gameMock := &mockExportGame{
		bundle: domain.Bundle{
			Filename:    "world1.mrpack",
			ContentType: "application/x-modrinth-modpack+zip",
			Data:        []byte("fake-mrpack-data"),
		},
	}
	h.cfg.MinecraftGame = gameMock

	// Valid export
	reqExport := httptest.NewRequest("GET", "/api/minecraft/1/mods/export", nil)
	respExport, err := app.Test(reqExport)
	if err != nil || respExport.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for export, got: %v, code: %d", err, respExport.StatusCode)
	}
	bExport, _ := io.ReadAll(respExport.Body)
	if string(bExport) != "fake-mrpack-data" {
		t.Errorf("expected fake-mrpack-data, got: %s", bExport)
	}

	// Invalid number via appBad
	respBadExp, _ := appBad.Test(httptest.NewRequest("GET", "/test/bad/export", nil))
	if respBadExp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for bad export num, got: %d", respBadExp.StatusCode)
	}

	// Unconfigured MinecraftGame
	hNoGame := New(Config{MCInstances: mgr})
	appNoGame := fiber.New()
	hNoGame.Register(appNoGame)
	respNoGame, _ := appNoGame.Test(httptest.NewRequest("GET", "/api/minecraft/1/mods/export", nil))
	if respNoGame.StatusCode != fiber.StatusServiceUnavailable {
		t.Errorf("expected 503 for unconfigured game export, got: %d", respNoGame.StatusCode)
	}

	// Instance not found
	respExpNotFound, _ := app.Test(httptest.NewRequest("GET", "/api/minecraft/999/mods/export", nil))
	if respExpNotFound.StatusCode != fiber.StatusNotFound {
		t.Errorf("expected 404 for missing export instance, got: %d", respExpNotFound.StatusCode)
	}

	// ExportClientBundle failure
	gameMock.exportErr = errors.New("zip creation failed")
	respExpFail, _ := app.Test(httptest.NewRequest("GET", "/api/minecraft/1/mods/export", nil))
	if respExpFail.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500 for export failure, got: %d", respExpFail.StatusCode)
	}

	_ = inst
}

func TestMCConfigsAllBranches(t *testing.T) {
	h, st, _, mgr, mockState := setupTestInstancesHandlerWithState(t)
	defer st.Close()

	// Seed instance
	_, _ = mgr.CreateInstance(context.Background(), domain.Instance{
		Number:    1,
		Name:      "ConfigWorld",
		State:     domain.StateStopped,
		Tier:      domain.TierSmall,
		MCVersion: "1.21.1",
	}, "")

	app := fiber.New()
	h.RegisterProtected(app)

	appBad := fiber.New()
	appBad.Get("/test/:num/configs/file", h.MCInstanceConfigGet)
	appBad.Post("/test/:num/configs/save", h.MCInstanceConfigSave)
	appBad.Post("/test/:num/configs/delete", h.MCInstanceConfigDelete)

	// 1. MCConfigsList when MCInstances is nil
	hNil := New(Config{})
	appNil := fiber.New()
	hNil.RegisterProtected(appNil)
	appNil.Get("/test-list", func(c *fiber.Ctx) error {
		files, err := hNil.MCConfigsList(c)
		if err != nil || files != nil {
			t.Errorf("expected nil, nil for nil MCInstances, got: %v, %v", files, err)
		}
		return c.SendString("ok")
	})
	respNilList, _ := appNil.Test(httptest.NewRequest("GET", "/test-list", nil))
	if respNilList.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for test-list, got: %d", respNilList.StatusCode)
	}

	// 2. MCConfigSave & Delete with unconfigured manager
	respUnconfSave, _ := appNil.Test(httptest.NewRequest("POST", "/minecraft/configs/save", nil))
	if respUnconfSave.StatusCode != fiber.StatusSeeOther {
		t.Errorf("expected 303 for unconf save, got: %d", respUnconfSave.StatusCode)
	}

	respUnconfDel, _ := appNil.Test(httptest.NewRequest("POST", "/minecraft/configs/delete", nil))
	if respUnconfDel.StatusCode != fiber.StatusSeeOther {
		t.Errorf("expected 303 for unconf delete, got: %d", respUnconfDel.StatusCode)
	}

	// 3. MCConfigSave with invalid regex file name
	formBadName := url.Values{"file": {"bad$name"}}
	reqBadSave := httptest.NewRequest("POST", "/minecraft/configs/save", strings.NewReader(formBadName.Encode()))
	reqBadSave.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respBadSave, _ := app.Test(reqBadSave)
	if respBadSave.StatusCode != fiber.StatusSeeOther {
		t.Errorf("expected 303 for bad save name, got: %d", respBadSave.StatusCode)
	}

	// 4. MCConfigDelete with invalid regex file name
	reqBadDel := httptest.NewRequest("POST", "/minecraft/configs/delete", strings.NewReader(formBadName.Encode()))
	reqBadDel.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respBadDel, _ := app.Test(reqBadDel)
	if respBadDel.StatusCode != fiber.StatusSeeOther {
		t.Errorf("expected 303 for bad delete name, got: %d", respBadDel.StatusCode)
	}

	// 5. MCConfigSave with unchanged file content
	formValid := url.Values{"file": {"server.properties"}, "content": {"difficulty=normal\n"}}
	reqValidSave := httptest.NewRequest("POST", "/minecraft/configs/save", strings.NewReader(formValid.Encode()))
	reqValidSave.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respValidSave, _ := app.Test(reqValidSave)
	if respValidSave.StatusCode != fiber.StatusSeeOther {
		t.Errorf("expected 303 for valid save, got: %d", respValidSave.StatusCode)
	}

	// 6. MCConfigDelete for file not present
	formNotPres := url.Values{"file": {"missing.properties"}}
	reqNotPres := httptest.NewRequest("POST", "/minecraft/configs/delete", strings.NewReader(formNotPres.Encode()))
	reqNotPres.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respNotPres, _ := app.Test(reqNotPres)
	if respNotPres.StatusCode != fiber.StatusSeeOther {
		t.Errorf("expected 303 for missing delete, got: %d", respNotPres.StatusCode)
	}

	// 7. MCConfigSave & Delete error branches
	mockState.patchErr = errors.New("simulated patch error")
	respSaveErr, _ := app.Test(reqValidSave)
	if respSaveErr.StatusCode != fiber.StatusSeeOther {
		t.Errorf("expected 303 on save error, got: %d", respSaveErr.StatusCode)
	}
	respDelErr, _ := app.Test(reqNotPres)
	if respDelErr.StatusCode != fiber.StatusSeeOther {
		t.Errorf("expected 303 on delete error, got: %d", respDelErr.StatusCode)
	}
	mockState.patchErr = nil

	// 8. MCInstanceConfigGet branches
	respNilGet, _ := appNil.Test(httptest.NewRequest("GET", "/api/minecraft/1/configs/file?f=server.properties", nil))
	if respNilGet.StatusCode != fiber.StatusServiceUnavailable {
		t.Errorf("expected 503 for nil get config, got: %d", respNilGet.StatusCode)
	}

	respBadGetNum, _ := appBad.Test(httptest.NewRequest("GET", "/test/bad/configs/file?f=server.properties", nil))
	if respBadGetNum.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for bad get num, got: %d", respBadGetNum.StatusCode)
	}

	respNoFileGet, _ := app.Test(httptest.NewRequest("GET", "/api/minecraft/1/configs/file?f=", nil))
	if respNoFileGet.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for empty get file, got: %d", respNoFileGet.StatusCode)
	}

	// patchSignals error seam in MCInstanceConfigGet
	origPatch := patchSignals
	defer func() { patchSignals = origPatch }()
	patchSignals = func(w *bufio.Writer, signals any) error {
		return errors.New("forced patchSignals error")
	}
	respErrPatch, _ := app.Test(httptest.NewRequest("GET", "/api/minecraft/1/configs/file?f=server.properties", nil))
	if respErrPatch.StatusCode == fiber.StatusOK {
		t.Errorf("expected non-OK on patchSignals error in config get")
	}
	patchSignals = origPatch

	// 9. MCInstanceConfigSave fallbacks and error paths
	respNilInstSave, _ := appNil.Test(httptest.NewRequest("POST", "/api/minecraft/1/configs/save", nil))
	bNilInstSave, _ := io.ReadAll(respNilInstSave.Body)
	if !strings.Contains(string(bNilInstSave), "Instance manager unconfigured") {
		t.Errorf("expected unconfigured toast, got: %s", bNilInstSave)
	}

	respBadInstSaveNum, _ := appBad.Test(httptest.NewRequest("POST", "/test/bad/configs/save", nil))
	bBadInstSaveNum, _ := io.ReadAll(respBadInstSaveNum.Body)
	if !strings.Contains(string(bBadInstSaveNum), "Invalid instance number") {
		t.Errorf("expected invalid instance number, got: %s", bBadInstSaveNum)
	}

	// fileName from FormValue fallback
	formInstSave := url.Values{"file": {"server.properties"}, "content": {"difficulty=normal\r\n"}}
	reqFormInstSave := httptest.NewRequest("POST", "/api/minecraft/1/configs/save", strings.NewReader(formInstSave.Encode()))
	reqFormInstSave.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respFormInstSave, _ := app.Test(reqFormInstSave)
	bFormInstSave, _ := io.ReadAll(respFormInstSave.Body)
	if !strings.Contains(string(bFormInstSave), "unchanged") && !strings.Contains(string(bFormInstSave), "Saved") {
		t.Errorf("expected unchanged or saved toast, got: %s", bFormInstSave)
	}

	// SaveConfig error via mockState.patchErr
	mockState.patchErr = errors.New("simulated patch failure")
	formNewContent := url.Values{"file": {"server.properties"}, "content": {"difficulty=hard\n"}}
	reqErrSave := httptest.NewRequest("POST", "/api/minecraft/1/configs/save", strings.NewReader(formNewContent.Encode()))
	reqErrSave.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respErrSave, _ := app.Test(reqErrSave)
	bErrSave, _ := io.ReadAll(respErrSave.Body)
	if !strings.Contains(string(bErrSave), "Save failed") {
		t.Errorf("expected Save failed toast, got: %s", bErrSave)
	}
	mockState.patchErr = nil

	// 10. MCInstanceConfigDelete fallbacks and error paths
	respNilInstDel, _ := appNil.Test(httptest.NewRequest("POST", "/api/minecraft/1/configs/delete", nil))
	bNilInstDel, _ := io.ReadAll(respNilInstDel.Body)
	if !strings.Contains(string(bNilInstDel), "Instance manager unconfigured") {
		t.Errorf("expected unconfigured toast, got: %s", bNilInstDel)
	}

	respBadInstDelNum, _ := appBad.Test(httptest.NewRequest("POST", "/test/bad/configs/delete", nil))
	bBadInstDelNum, _ := io.ReadAll(respBadInstDelNum.Body)
	if !strings.Contains(string(bBadInstDelNum), "Invalid instance number") {
		t.Errorf("expected invalid instance number, got: %s", bBadInstDelNum)
	}

	respNoFileDel, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/1/configs/delete", strings.NewReader(`{"file":""}`)))
	bNoFileDel, _ := io.ReadAll(respNoFileDel.Body)
	if !strings.Contains(string(bNoFileDel), "File name required") {
		t.Errorf("expected file name required toast, got: %s", bNoFileDel)
	}

	// Delete file error via mockState.patchErr
	mockState.patchErr = errors.New("simulated patch failure")
	reqErrDel := httptest.NewRequest("POST", "/api/minecraft/1/configs/delete", strings.NewReader(`{"file":"server.properties"}`))
	reqErrDel.Header.Set("Content-Type", "application/json")
	respErrDel, _ := app.Test(reqErrDel)
	bErrDel, _ := io.ReadAll(respErrDel.Body)
	if !strings.Contains(string(bErrDel), "Delete failed") {
		t.Errorf("expected delete failed toast, got: %s", bErrDel)
	}
	mockState.patchErr = nil

	// Delete file not present on instance 1
	reqNotPresInst := httptest.NewRequest("POST", "/api/minecraft/1/configs/delete", strings.NewReader(`{"file":"nonexistent.properties"}`))
	reqNotPresInst.Header.Set("Content-Type", "application/json")
	respNotPresInst, _ := app.Test(reqNotPresInst)
	bNotPresInst, _ := io.ReadAll(respNotPresInst.Body)
	if !strings.Contains(string(bNotPresInst), "was not present") && !strings.Contains(string(bNotPresInst), "Deleted") {
		t.Errorf("expected not present toast, got: %s", bNotPresInst)
	}
}

func TestMCModsSearchAllBranches(t *testing.T) {
	h, st, _, mgr := setupTestInstancesHandler(t)
	defer st.Close()

	// Seed instance with slug matching fake k8s ConfigMap ("mc-ducktopia-01-mods")
	_, _ = mgr.CreateInstance(context.Background(), domain.Instance{
		Number:    1,
		Name:      "Ducktopia",
		State:     domain.StateStopped,
		Tier:      domain.TierSmall,
		MCVersion: "1.21.1",
	}, "jei\n")

	app := fiber.New()
	h.RegisterProtected(app)

	appBad := fiber.New()
	appBad.Get("/test/:num/mods/search", h.MCInstanceModsSearch)

	// 1. Invalid instance number
	respBadNum, _ := appBad.Test(httptest.NewRequest("GET", "/test/bad/mods/search?q=jei", nil))
	if respBadNum.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for invalid number, got: %d", respBadNum.StatusCode)
	}

	// 2. SearchMods unconfigured
	hNoSearch := New(Config{MCInstances: mgr})
	appNoSearch := fiber.New()
	hNoSearch.RegisterProtected(appNoSearch)
	respNoSearch, _ := appNoSearch.Test(httptest.NewRequest("GET", "/api/minecraft/1/mods/search?q=jei", nil))
	bNoSearch, _ := io.ReadAll(respNoSearch.Body)
	if !strings.Contains(string(bNoSearch), "Mod search is unconfigured") {
		t.Errorf("expected unconfigured message, got: %s", bNoSearch)
	}

	// 3. Search query empty
	h.cfg.SearchMods = func(ctx context.Context, query, mcVersion, loader string) ([]ModHit, error) {
		if query == "err" {
			return nil, errors.New("modrinth backend error")
		}
		if query == "empty" {
			return nil, nil
		}
		return []ModHit{
			{
				Slug:        "jei",
				Title:       "Just Enough Items",
				Description: "Item viewer",
				IconURL:     "https://icon.png",
			},
			{
				Slug:        "appleskin",
				Title:       "AppleSkin",
				Description: "Food HUD",
				IconURL:     "https://apple.png",
			},
		}, nil
	}

	respEmptyQ, _ := app.Test(httptest.NewRequest("GET", "/api/minecraft/1/mods/search?q=", nil))
	bEmptyQ, _ := io.ReadAll(respEmptyQ.Body)
	if !strings.Contains(string(bEmptyQ), "Search Modrinth to find mods") {
		t.Errorf("expected empty search prompt, got: %s", bEmptyQ)
	}

	// 4. Search query error
	respErrQ, _ := app.Test(httptest.NewRequest("GET", "/api/minecraft/1/mods/search?q=err", nil))
	bErrQ, _ := io.ReadAll(respErrQ.Body)
	if !strings.Contains(string(bErrQ), "Search failed") {
		t.Errorf("expected Search failed message, got: %s", bErrQ)
	}

	// 5. 0 hits
	respNone, _ := app.Test(httptest.NewRequest("GET", "/api/minecraft/1/mods/search?q=empty", nil))
	bNone, _ := io.ReadAll(respNone.Body)
	if !strings.Contains(string(bNone), "No mods found") {
		t.Errorf("expected No mods found message, got: %s", bNone)
	}

	// 6. Hits found: "jei" is installed, "appleskin" is not -> shows Installed and + Install
	respHits, _ := app.Test(httptest.NewRequest("GET", "/api/minecraft/1/mods/search?q=jei", nil))
	bHits, _ := io.ReadAll(respHits.Body)
	if !strings.Contains(string(bHits), "Installed") || !strings.Contains(string(bHits), "+ Install") {
		t.Errorf("expected installed badge and install button, got: %s", bHits)
	}

	// 7. innerElement error seam in patchModResults
	origInner := innerElement
	defer func() { innerElement = origInner }()
	innerElement = func(w *bufio.Writer, selector, content string) error {
		return errors.New("forced patch inner error")
	}
	respErrInner, _ := app.Test(httptest.NewRequest("GET", "/api/minecraft/1/mods/search?q=jei", nil))
	if respErrInner.StatusCode == fiber.StatusOK {
		t.Errorf("expected non-OK on innerElement error in mod search")
	}
	innerElement = origInner
}

func TestMCModUpdatesAllBranches(t *testing.T) {
	h, st, _, mgr := setupTestInstancesHandler(t)
	defer st.Close()

	repo := store.NewInstanceRepo(st)

	// 1. Seed modlist instance with slug "ducktopia" matching fake k8s ConfigMap
	_ = repo.Upsert(domain.Instance{
		Number:    1,
		Name:      "Ducktopia",
		Slug:      "ducktopia",
		State:     domain.StateRunning,
		Tier:      domain.TierSmall,
		MCVersion: "1.21.1",
		Loader:    domain.LoaderFabric,
		Source:    domain.SourceModlist,
	})

	// 2. Seed modpack instance
	_ = repo.Upsert(domain.Instance{
		Number:    2,
		Name:      "PackWorld",
		State:     domain.StateStopped,
		Tier:      domain.TierSmall,
		MCVersion: "1.21.1",
		Source:    domain.SourceModpack,
		Pack: &domain.Pack{
			Name: "ATM9",
			Ref:  "atm-9",
		},
	})

	// 3. Seed vanilla instance
	_ = repo.Upsert(domain.Instance{
		Number:    3,
		Name:      "VanillaWorld",
		State:     domain.StateStopped,
		Tier:      domain.TierSmall,
		MCVersion: "1.21.1",
		Source:    domain.SourceVanilla,
	})

	app := fiber.New()
	h.RegisterProtected(app)

	appBad := fiber.New()
	appBad.Post("/test/:num/updates/check", h.MCModUpdatesCheck)
	appBad.Post("/test/:num/updates/apply", h.MCModUpdatesApply)
	appBad.Post("/test/:num/updates/undo", h.MCModUpdatesUndo)

	// updatableInstance edge cases:
	// - unconfigured
	hNil := New(Config{})
	appNil := fiber.New()
	hNil.RegisterProtected(appNil)
	respNilCheck, _ := appNil.Test(httptest.NewRequest("POST", "/api/minecraft/1/mods/updates/check", nil))
	bNilCheck, _ := io.ReadAll(respNilCheck.Body)
	if !strings.Contains(string(bNilCheck), "unconfigured") {
		t.Errorf("expected unconfigured error, got: %s", bNilCheck)
	}

	// - invalid number via appBad
	respBadNum, _ := appBad.Test(httptest.NewRequest("POST", "/test/bad/updates/check", nil))
	bBadNum, _ := io.ReadAll(respBadNum.Body)
	if !strings.Contains(string(bBadNum), "invalid instance number") {
		t.Errorf("expected invalid instance number, got: %s", bBadNum)
	}

	// - not found
	respNotFound, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/999/mods/updates/check", nil))
	bNotFound, _ := io.ReadAll(respNotFound.Body)
	if !strings.Contains(string(bNotFound), "not found") {
		t.Errorf("expected not found, got: %s", bNotFound)
	}

	// - vanilla instance immutable error
	respVanilla, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/3/mods/updates/check", nil))
	bVanilla, _ := io.ReadAll(respVanilla.Body)
	if !strings.Contains(string(bVanilla), "is a vanilla world") {
		t.Errorf("expected vanilla immutable error, got: %s", bVanilla)
	}

	// - modpack instance managed by pack error
	respPack, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/2/mods/updates/check", nil))
	bPack, _ := io.ReadAll(respPack.Body)
	if !strings.Contains(string(bPack), "managed by the modpack") {
		t.Errorf("expected managed by modpack error, got: %s", bPack)
	}

	// Configure ModUpdates
	cat := &mockModUpdatesCatalog{
		versions: map[string]string{"jei|fabric": "16.0.0"},
	}
	checker := modupdates.New(mgr, cat, modupdates.WithGameID(domain.GameMinecraft), modupdates.WithRestorePoints(st))
	h.cfg.ModUpdates = checker

	// MCModUpdatesCheck: updates available on instance 1
	respCheck, err := app.Test(httptest.NewRequest("POST", "/api/minecraft/1/mods/updates/check", nil))
	if err != nil || respCheck.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for update check, got: %v, code: %d", err, respCheck.StatusCode)
	}

	// MCModUpdatesApply:
	// 1. Unconfigured checker
	hNoUp := New(Config{MCInstances: mgr})
	appNoUp := fiber.New()
	hNoUp.RegisterProtected(appNoUp)
	respNoUpApply, _ := appNoUp.Test(httptest.NewRequest("POST", "/api/minecraft/1/mods/updates/apply", nil))
	bNoUpApply, _ := io.ReadAll(respNoUpApply.Body)
	if !strings.Contains(string(bNoUpApply), "not configured") {
		t.Errorf("expected not configured toast, got: %s", bNoUpApply)
	}

	// 2. Empty selection
	respEmptySel, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/1/mods/updates/apply", strings.NewReader(`{"all":false,"selected":{}}`)))
	bEmptySel, _ := io.ReadAll(respEmptySel.Body)
	if !strings.Contains(string(bEmptySel), "No mods selected to update") {
		t.Errorf("expected no mods selected toast, got: %s", bEmptySel)
	}

	// 3. Selected matching token
	tok := pages.UpdateToken("jei")
	reqSel := httptest.NewRequest("POST", "/api/minecraft/1/mods/updates/apply", strings.NewReader(fmt.Sprintf(`{"all":false,"selected":{"%s":true}}`, tok)))
	reqSel.Header.Set("Content-Type", "application/json")
	respSel, _ := app.Test(reqSel)
	if respSel.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for selected update apply, got: %d", respSel.StatusCode)
	}

	// 4. MCModUpdatesUndo:
	// Unconfigured
	respNoUpUndo, _ := appNoUp.Test(httptest.NewRequest("POST", "/api/minecraft/1/mods/updates/undo", nil))
	bNoUpUndo, _ := io.ReadAll(respNoUpUndo.Body)
	if !strings.Contains(string(bNoUpUndo), "not configured") {
		t.Errorf("expected not configured toast, got: %s", bNoUpUndo)
	}

	// Valid Undo
	respUndo, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/1/mods/updates/undo", nil))
	if respUndo.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for undo, got: %d", respUndo.StatusCode)
	}

	// pushUpdatePanel seam errors:
	// 1. renderUpdatePanel error seam
	origRender := renderUpdatePanel
	defer func() { renderUpdatePanel = origRender }()
	renderUpdatePanel = func(ctx context.Context, d pages.InstanceDetailUI, w io.Writer) error {
		return errors.New("forced render panel error")
	}
	respErrRender, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/1/mods/updates/check", nil))
	bErrRender, _ := io.ReadAll(respErrRender.Body)
	if !strings.Contains(string(bErrRender), "Render failed") {
		t.Errorf("expected Render failed toast, got: %s", bErrRender)
	}
	renderUpdatePanel = origRender

	// 2. innerElement error seam
	origInner := innerElement
	defer func() { innerElement = origInner }()
	innerElement = func(w *bufio.Writer, selector, content string) error {
		return errors.New("forced panel inner error")
	}
	respErrInner, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/1/mods/updates/check", nil))
	if respErrInner.StatusCode == fiber.StatusOK {
		t.Errorf("expected non-OK on innerElement error in update panel")
	}
	innerElement = origInner

	// 3. patchSignals error seam
	origSignals := patchSignals
	defer func() { patchSignals = origSignals }()
	patchSignals = func(w *bufio.Writer, signals any) error {
		return errors.New("forced panel signals error")
	}
	respErrSig, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/1/mods/updates/check", nil))
	if respErrSig.StatusCode == fiber.StatusOK {
		t.Errorf("expected non-OK on patchSignals error in update panel")
	}
	patchSignals = origSignals
}

type mockAdvModUpdatesCatalog struct {
	latestFn func(ctx context.Context, ref domain.ModRef, inst domain.Instance) (string, []string, error)
	treeFn   func(ctx context.Context, ref domain.ModRef, inst domain.Instance) ([]string, error)
}

func (m *mockAdvModUpdatesCatalog) LatestVersion(ctx context.Context, ns, name string) (string, []string, error) {
	return "", nil, nil
}
func (m *mockAdvModUpdatesCatalog) ResolveTree(ctx context.Context, ns, name string) ([]string, error) {
	return nil, nil
}
func (m *mockAdvModUpdatesCatalog) LatestVersionForInstance(ctx context.Context, ref domain.ModRef, inst domain.Instance) (string, []string, error) {
	if m.latestFn != nil {
		return m.latestFn(ctx, ref, inst)
	}
	return "", nil, nil
}
func (m *mockAdvModUpdatesCatalog) ResolveTreeForInstance(ctx context.Context, ref domain.ModRef, inst domain.Instance) ([]string, error) {
	if m.treeFn != nil {
		return m.treeFn(ctx, ref, inst)
	}
	return nil, nil
}

func TestMinecraftFinalCoverageEdges(t *testing.T) {
	h, st, mck8s, mgr, mockState := setupTestInstancesHandlerWithState(t)
	defer st.Close()

	_, err := mgr.CreateInstance(context.Background(), domain.Instance{
		Number:    1,
		Name:      "Ducktopia",
		MCVersion: "1.21.1",
		Loader:    domain.LoaderNeoForge,
		Tier:      domain.TierMedium,
		State:     domain.StateRunning,
	}, "jei\n")
	if err != nil {
		t.Fatal(err)
	}

	// 1. configs.go: MCInstanceConfigSave when req.File is empty, fallback to c.FormValue("file")
	app := fiber.New()
	h.RegisterProtected(app)
	reqCfgFormFile := httptest.NewRequest("POST", "/api/minecraft/1/configs/save?file=server.properties", strings.NewReader(`{"content":"difficulty=hard"}`))
	reqCfgFormFile.Header.Set("Content-Type", "application/json")
	respCfgFormFile, err := app.Test(reqCfgFormFile)
	if err != nil || respCfgFormFile.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for config save with form file fallback, got: %v, code: %d", err, respCfgFormFile.StatusCode)
	}

	// 2. detail.go: MCInstancePage when tab is whitespace
	appUnescape := fiber.New(fiber.Config{UnescapePath: true})
	h.RegisterProtected(appUnescape)
	reqTabWS := httptest.NewRequest("GET", "/minecraft/1/%20", nil)
	respTabWS, err := appUnescape.Test(reqTabWS)
	if err != nil || respTabWS.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for whitespace tab, got: %v, code: %d", err, respTabWS.StatusCode)
	}

	// 3. detail.go: MCInstancePage when tab == "backups" and backups exist
	bDir := testBackupsDir
	inst1, _ := mgr.GetInstance(context.Background(), 1)
	bPath := filepath.Join(bDir, fmt.Sprintf("mc-%s-%02d-20260914.tar.gz", inst1.Slug, inst1.Number))
	if err := os.WriteFile(bPath, []byte("fake backup archive"), 0644); err != nil {
		t.Fatal(err)
	}
	reqBackupsTab := httptest.NewRequest("GET", "/minecraft/1/backups", nil)
	respBackupsTab, err := app.Test(reqBackupsTab)
	if err != nil || respBackupsTab.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for backups tab with backups, got: %v, code: %d", err, respBackupsTab.StatusCode)
	}

	// 4. detail.go: MCInstanceSettingsSave when req.Name is empty, fallback to c.FormValue("name")
	reqSettingsFormName := httptest.NewRequest("POST", "/api/minecraft/1/settings?name=NewDuckName", strings.NewReader(`{"motd":"fresh motd"}`))
	reqSettingsFormName.Header.Set("Content-Type", "application/json")
	respSettingsFormName, err := app.Test(reqSettingsFormName)
	if err != nil || respSettingsFormName.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for settings save with form name fallback, got: %v, code: %d", err, respSettingsFormName.StatusCode)
	}

	// 5. detail.go: MCInstanceExport when :num is invalid on unconstrained route
	appUnconstrained := fiber.New()
	appUnconstrained.Get("/test/export/:num", h.MCInstanceExport)
	appUnconstrained.Get("/test/page/:num/:tab?", h.MCInstancePage)
	appUnconstrained.Post("/test/apply/:num", h.MCModUpdatesApply)
	appUnconstrained.Post("/test/undo/:num", h.MCModUpdatesUndo)

	respExportBadNum, _ := appUnconstrained.Test(httptest.NewRequest("GET", "/test/export/bad", nil))
	if respExportBadNum.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for export with bad num, got: %d", respExportBadNum.StatusCode)
	}

	respPageEmptyTab, err := appUnconstrained.Test(httptest.NewRequest("GET", "/test/page/1", nil))
	if err != nil || respPageEmptyTab.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for page with empty tab fallback, got: %v, code: %d", err, respPageEmptyTab.StatusCode)
	}

	// 6. dashboard.go: MCDashboard budget branches
	// Branch A: budget.RunningCount >= budget.MaxRunning
	// Create a manager with maxRunning = 1, create 2 instances (1 running, 1 stopped)
	stA, err := store.Open(filepath.Join(t.TempDir(), "testA.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stA.Close()
	mgrA := instances.NewInstanceManager(
		store.NewInstanceRepo(stA), mockState, nil, 24, 4, 1, "manifests/minecraft-modded", "192.168.20.224", manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded"), "minecraft-modded",
	)
	_, _ = mgrA.CreateInstance(context.Background(), domain.Instance{
		Number: 1, Name: "Running1", State: domain.StateRunning, Tier: domain.TierSmall, MCVersion: "1.21.1",
	}, "")
	_, _ = mgrA.CreateInstance(context.Background(), domain.Instance{
		Number: 2, Name: "Stopped2", State: domain.StateStopped, Tier: domain.TierSmall, MCVersion: "1.21.1",
	}, "")
	hA := New(Config{MCInstances: mgrA})
	appA := fiber.New()
	hA.RegisterProtected(appA)
	respDashA, _ := appA.Test(httptest.NewRequest("GET", "/minecraft", nil))
	bDashA, _ := io.ReadAll(respDashA.Body)
	if !strings.Contains(string(bDashA), "Max 1 running instances reached") {
		t.Errorf("expected max running reached in dashboard, got: %s", bDashA)
	}

	// Branch B: budget.UsedGiB + inst.MemoryGiB() > budget.TotalBudgetGiB
	// Create a manager with totalBudgetGiB = 6, maxRunning = 5
	// inst 1: Running, TierMedium (4GiB). inst 2: Stopped, TierLarge (8GiB). 4 + 8 = 12 > 6
	stB, err := store.Open(filepath.Join(t.TempDir(), "testB.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stB.Close()
	mgrB := instances.NewInstanceManager(
		store.NewInstanceRepo(stB), mockState, nil, 6, 4, 5, "manifests/minecraft-modded", "192.168.20.224", manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded"), "minecraft-modded",
	)
	_, _ = mgrB.CreateInstance(context.Background(), domain.Instance{
		Number: 1, Name: "RunningB1", State: domain.StateRunning, Tier: domain.TierMedium, MCVersion: "1.21.1",
	}, "")
	_, _ = mgrB.CreateInstance(context.Background(), domain.Instance{
		Number: 2, Name: "StoppedB2", State: domain.StateStopped, Tier: domain.TierLarge, MCVersion: "1.21.1",
	}, "")
	hB := New(Config{MCInstances: mgrB})
	appB := fiber.New()
	hB.RegisterProtected(appB)
	respDashB, _ := appB.Test(httptest.NewRequest("GET", "/minecraft", nil))
	bDashB, _ := io.ReadAll(respDashB.Body)
	if !strings.Contains(string(bDashB), "Exceeds 6 GiB RAM budget") {
		t.Errorf("expected exceeds RAM budget in dashboard, got: %s", bDashB)
	}

	// 7. updates.go: MCModUpdatesCheck
	// Branch A: h.cfg.ModUpdates == nil
	hNoModUp := New(Config{MCInstances: mgr})
	appNoModUp := fiber.New()
	hNoModUp.RegisterProtected(appNoModUp)
	respCheckNoModUp, _ := appNoModUp.Test(httptest.NewRequest("POST", "/api/minecraft/1/mods/updates/check", nil))
	bCheckNoModUp, _ := io.ReadAll(respCheckNoModUp.Body)
	if !strings.Contains(string(bCheckNoModUp), "Mod update checking is not configured") {
		t.Errorf("expected not configured toast, got: %s", bCheckNoModUp)
	}

	// Branch B: Missing, Unreachable, and Updates present in report
	var simMissing bool
	advCat := &mockAdvModUpdatesCatalog{
		latestFn: func(ctx context.Context, ref domain.ModRef, inst domain.Instance) (string, []string, error) {
			switch ref.Name {
			case "mod-unreachable":
				if simMissing {
					return "", nil, ports.ErrPackageNotFound
				}
				return "", nil, errors.New("timeout connecting")
			case "mod-update":
				return "2.0.0", nil, nil
			default:
				return "", nil, nil
			}
		},
		treeFn: func(ctx context.Context, ref domain.ModRef, inst domain.Instance) ([]string, error) {
			return []string{ref.Name + ":2.0.0"}, nil
		},
	}

	// Setup an instance with mod-unreachable and mod-update
	_, err = mgr.CreateInstance(context.Background(), domain.Instance{
		Number:    2,
		Name:      "ModdedWorld2",
		State:     domain.StateRunning,
		MCVersion: "1.21.1",
		Loader:    domain.LoaderNeoForge,
	}, "mod-unreachable\nmod-update:1.0.0\n")
	if err != nil {
		t.Fatal(err)
	}

	cm2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "mc-moddedworld2-02-mods", Namespace: "minecraft-modded"},
		Data: map[string]string{
			"mods.txt": "mod-unreachable\nmod-update:1.0.0\n",
		},
	}
	_, _ = mck8s.Clientset().CoreV1().ConfigMaps("minecraft-modded").Create(context.Background(), cm2, metav1.CreateOptions{})

	checker := modupdates.New(mgr, advCat, modupdates.WithGameID(domain.GameMinecraft), modupdates.WithRestorePoints(st))
	h.cfg.ModUpdates = checker

	// Check on instance 2: unreachable + update -> hits unreachable branch
	respCheckUnreachable, err := app.Test(httptest.NewRequest("POST", "/api/minecraft/2/mods/updates/check", nil))
	if err != nil || respCheckUnreachable.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for unreachable update check, got: %v, code: %d", err, respCheckUnreachable.StatusCode)
	}
	bCheckUnreachable, _ := io.ReadAll(respCheckUnreachable.Body)
	if !strings.Contains(string(bCheckUnreachable), "could not be checked") {
		t.Errorf("expected unreachable message, got: %s", bCheckUnreachable)
	}

	// Second check on instance 2 with simMissing=true -> hits missing branch
	simMissing = true
	respCheckMissing, err := app.Test(httptest.NewRequest("POST", "/api/minecraft/2/mods/updates/check", nil))
	if err != nil || respCheckMissing.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for missing update check, got: %v, code: %d", err, respCheckMissing.StatusCode)
	}
	bCheckMissing, _ := io.ReadAll(respCheckMissing.Body)
	if !strings.Contains(string(bCheckMissing), "gone from Modrinth") {
		t.Errorf("expected missing message, got: %s", bCheckMissing)
	}
	simMissing = false

	// 8. updates.go: MCModUpdatesCheck RefreshOne error
	// Create instance 3 whose mods CM does not exist, so GetInstalledMods errors
	_, err = mgr.CreateInstance(context.Background(), domain.Instance{
		Number:    3,
		Name:      "WorldNoCM",
		State:     domain.StateRunning,
		MCVersion: "1.21.1",
		Loader:    domain.LoaderNeoForge,
	}, "some-mod\n")
	if err != nil {
		t.Fatal(err)
	}
	respCheckErr, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/3/mods/updates/check", nil))
	bCheckErr, _ := io.ReadAll(respCheckErr.Body)
	if !strings.Contains(string(bCheckErr), "Check failed:") {
		t.Errorf("expected Check failed toast, got: %s", bCheckErr)
	}

	// 9. updates.go: ModUpdateState lines 35-37 (LastError != nil)
	// Now instance 3 has LastError set on checker!
	dUI := pages.InstanceDetailUI{}
	inst3, _ := mgr.GetInstance(context.Background(), 3)
	h.ModUpdateState(context.Background(), &dUI, *inst3)
	if !strings.Contains(dUI.UpdatesError, "Last check failed:") {
		t.Errorf("expected UpdatesError to be set on dUI, got: %s", dUI.UpdatesError)
	}

	// 10. updates.go: ModUpdateState lines 39-42 (RestoreAvailable != nil) and MCModUpdatesUndo lines 140-142 (Undo success)
	// Create instance 4 with mods that match a restore point
	_, err = mgr.CreateInstance(context.Background(), domain.Instance{
		Number:    4,
		Name:      "WorldRestore",
		State:     domain.StateRunning,
		MCVersion: "1.21.1",
		Loader:    domain.LoaderNeoForge,
	}, "restored-mod:2.0.0\n")
	if err != nil {
		t.Fatal(err)
	}
	cm4 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "mc-worldrestore-04-mods", Namespace: "minecraft-modded"},
		Data: map[string]string{
			"mods.txt": "restored-mod:2.0.0\n",
		},
	}
	_, _ = mck8s.Clientset().CoreV1().ConfigMaps("minecraft-modded").Create(context.Background(), cm4, metav1.CreateOptions{})
	// Save restore point in store
	_ = st.SaveRestorePoint(domain.GameMinecraft, 4, []string{"restored-mod:1.0.0"}, []string{"restored-mod:2.0.0"})

	dUI4 := pages.InstanceDetailUI{}
	inst4, _ := mgr.GetInstance(context.Background(), 4)
	h.ModUpdateState(context.Background(), &dUI4, *inst4)
	if !dUI4.CanUndo || !strings.Contains(dUI4.UndoWhen, "ago") {
		t.Errorf("expected CanUndo=true and UndoWhen set, got: %+v", dUI4)
	}

	// Now call Undo on instance 4 -> hits lines 140-142!
	respUndo4, err := app.Test(httptest.NewRequest("POST", "/api/minecraft/4/mods/updates/undo", nil))
	if err != nil || respUndo4.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for successful undo, got: %v, code: %d", err, respUndo4.StatusCode)
	}
	bUndo4, _ := io.ReadAll(respUndo4.Body)
	if !strings.Contains(string(bUndo4), "Reverted to the 1 mod(s)") {
		t.Errorf("expected revert message in undo response, got: %s", bUndo4)
	}

	// 11. updates.go: MCModUpdatesApply
	// Branch: updatableInstance fails (bad num on unconstrained route)
	respApplyBadNum, _ := appUnconstrained.Test(httptest.NewRequest("POST", "/test/apply/bad", nil))
	bApplyBadNum, _ := io.ReadAll(respApplyBadNum.Body)
	if !strings.Contains(string(bApplyBadNum), "invalid instance number") {
		t.Errorf("expected invalid instance number toast, got: %s", bApplyBadNum)
	}

	// Branch: updatableInstance fails for undo (bad num on unconstrained route)
	respUndoBadNum, _ := appUnconstrained.Test(httptest.NewRequest("POST", "/test/undo/bad", nil))
	bUndoBadNum, _ := io.ReadAll(respUndoBadNum.Body)
	if !strings.Contains(string(bUndoBadNum), "invalid instance number") {
		t.Errorf("expected invalid instance number toast, got: %s", bUndoBadNum)
	}

	// Branch: Apply returns error (e.g. mockState.patchErr)
	mockState.patchErr = errors.New("simulated patch error for apply")
	respApplyErr, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/2/mods/updates/apply", strings.NewReader(`{"all":true}`)))
	bApplyErr, _ := io.ReadAll(respApplyErr.Body)
	if !strings.Contains(string(bApplyErr), "Update failed: simulated patch error") {
		t.Errorf("expected Update failed toast, got: %s", bApplyErr)
	}
	mockState.patchErr = nil

	// Branch: Pending is true
	// Make a successful Apply on instance 2 with all: true.
	respApply1, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/2/mods/updates/apply", strings.NewReader(`{"all":true}`)))
	if respApply1.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for apply 1, got: %d", respApply1.StatusCode)
	}
	// Immediately make a 2nd Apply while pending
	respApply2, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/2/mods/updates/apply", strings.NewReader(`{"all":true}`)))
	bApply2, _ := io.ReadAll(respApply2.Body)
	if !strings.Contains(string(bApply2), "An update is already in progress") {
		t.Errorf("expected already in progress toast, got: %s", bApply2)
	}

	// Branch: len(applied) == 0 ("Already up to date.")
	// Instance 1 has no updates available
	respApplyZero, _ := app.Test(httptest.NewRequest("POST", "/api/minecraft/1/mods/updates/apply", strings.NewReader(`{"all":true}`)))
	bApplyZero, _ := io.ReadAll(respApplyZero.Body)
	if !strings.Contains(string(bApplyZero), "Already up to date") {
		t.Errorf("expected Already up to date toast, got: %s", bApplyZero)
	}
}

