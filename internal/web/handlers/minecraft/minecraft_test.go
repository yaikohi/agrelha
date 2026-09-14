package minecraft

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

type mockMCStateStore struct {
	docs map[string]ports.Document
}

func (s *mockMCStateStore) Get(_ context.Context, path string) (ports.Document, error) {
	if s.docs == nil {
		return ports.Document{Data: make(map[string]string)}, nil
	}
	return s.docs[path], nil
}
func (s *mockMCStateStore) Put(_ context.Context, path string, doc ports.Document, _ string) error {
	if s.docs == nil {
		s.docs = make(map[string]ports.Document)
	}
	s.docs[path] = doc
	return nil
}
func (s *mockMCStateStore) Patch(ctx context.Context, path, msg string, fn func(*ports.Document) (bool, error)) (bool, error) {
	if s.docs == nil {
		s.docs = make(map[string]ports.Document)
	}
	doc := s.docs[path]
	if doc.Data == nil {
		doc.Data = make(map[string]string)
	}
	changed, err := fn(&doc)
	if err != nil {
		return false, err
	}
	if changed {
		s.docs[path] = doc
	}
	return changed, nil
}
func (s *mockMCStateStore) Delete(_ context.Context, path, _ string) error {
	delete(s.docs, path)
	return nil
}
func (s *mockMCStateStore) PutTree(_ context.Context, _ string, tree map[string]ports.Document, _ string) error {
	if s.docs == nil {
		s.docs = make(map[string]ports.Document)
	}
	for k, v := range tree {
		s.docs[k] = v
	}
	return nil
}

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
	mockState := &mockMCStateStore{docs: make(map[string]ports.Document)}
	var mgr *instances.InstanceManager
	mgr = instances.NewInstanceManager(
		store.NewInstanceRepo(st), mockState, k8sruntime.New(mck8s), 24, 4, 2, "manifests/minecraft-modded", "192.168.20.224", manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded"), "minecraft-modded",
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

	// 2. Apply updates
	reqApply := httptest.NewRequest("POST", "/api/minecraft/1/mods/updates/apply?mod=jei", nil)
	respApply, err := app.Test(reqApply)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if respApply.StatusCode != 200 {
		t.Errorf("apply status = %d, want 200", respApply.StatusCode)
	}

	// 3. Undo updates
	reqUndo := httptest.NewRequest("POST", "/api/minecraft/1/mods/updates/undo", nil)
	respUndo, err := app.Test(reqUndo)
	if err != nil {
		t.Fatalf("undo failed: %v", err)
	}
	if respUndo.StatusCode != 200 {
		t.Errorf("undo status = %d, want 200", respUndo.StatusCode)
	}
}

func TestMCInstancePageTabsAndRedirects(t *testing.T) {
	h, st, _, mgr := setupTestInstancesHandler(t)
	defer st.Close()

	_, err := mgr.CreateInstance(context.Background(), domain.Instance{
		Name:      "Ducktopia",
		MCVersion: "1.21.1",
		Loader:    domain.LoaderNeoForge,
		Tier:      domain.TierMedium,
		State:     domain.StateStopped,
	}, "jei\n")
	if err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	h.Register(app)

	// 1. Redirect /minecraft/1 -> /minecraft/1/overview
	reqRedirect := httptest.NewRequest("GET", "/minecraft/1", nil)
	respRedirect, _ := app.Test(reqRedirect)
	if respRedirect.StatusCode != fiber.StatusFound && respRedirect.StatusCode != fiber.StatusMovedPermanently {
		t.Errorf("expected redirect for /minecraft/1, got: %d", respRedirect.StatusCode)
	}

	// 2. Legacy redirects
	reqLegacyMods := httptest.NewRequest("GET", "/minecraft/mods", nil)
	respLegacyMods, _ := app.Test(reqLegacyMods)
	if respLegacyMods.StatusCode != fiber.StatusTemporaryRedirect {
		t.Errorf("expected temporary redirect for /minecraft/mods, got: %d", respLegacyMods.StatusCode)
	}

	reqLegacyConfigs := httptest.NewRequest("GET", "/minecraft/configs", nil)
	respLegacyConfigs, _ := app.Test(reqLegacyConfigs)
	if respLegacyConfigs.StatusCode != fiber.StatusTemporaryRedirect {
		t.Errorf("expected temporary redirect for /minecraft/configs, got: %d", respLegacyConfigs.StatusCode)
	}

	// 3. Tabs
	tabs := []string{"overview", "configs", "mods", "backups", "terminal", "danger"}
	for _, tab := range tabs {
		reqTab := httptest.NewRequest("GET", fmt.Sprintf("/minecraft/1/%s", tab), nil)
		respTab, err := app.Test(reqTab)
		if err != nil || respTab.StatusCode != fiber.StatusOK {
			t.Errorf("tab %s failed: status=%d, err=%v", tab, respTab.StatusCode, err)
		}
	}

	// 4. Non-existent instance
	reqNotFound := httptest.NewRequest("GET", "/minecraft/99/overview", nil)
	respNotFound, _ := app.Test(reqNotFound)
	if respNotFound.StatusCode != fiber.StatusNotFound {
		t.Errorf("expected 404 for missing instance, got: %d", respNotFound.StatusCode)
	}
}

func TestMCInstanceSettingsAndModInstallRemove(t *testing.T) {
	h, st, _, mgr := setupTestInstancesHandler(t)
	defer st.Close()

	_, err := mgr.CreateInstance(context.Background(), domain.Instance{
		Name:      "Ducktopia",
		MCVersion: "1.21.1",
		Loader:    domain.LoaderNeoForge,
		Tier:      domain.TierMedium,
		State:     domain.StateStopped,
	}, "jei\n")
	if err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	h.Register(app)

	// 1. Settings Save
	reqSettings := httptest.NewRequest("POST", "/api/minecraft/1/settings", strings.NewReader(`name=Ducktopia+Updated&motd=New+MOTD&tier=large&mc_version=1.21.1`))
	reqSettings.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respSettings, err := app.Test(reqSettings)
	if err != nil || respSettings.StatusCode != fiber.StatusOK {
		t.Fatalf("settings save failed: %v, status: %d", err, respSettings.StatusCode)
	}

	// 2. Mod Install
	reqInstall := httptest.NewRequest("POST", "/api/minecraft/1/mods/install", strings.NewReader(`{"slug":"ferrite-core"}`))
	reqInstall.Header.Set("Content-Type", "application/json")
	respInstall, err := app.Test(reqInstall)
	if err != nil || respInstall.StatusCode != fiber.StatusOK {
		t.Fatalf("mod install failed: %v, status: %d", err, respInstall.StatusCode)
	}

	// 3. Mod Remove
	reqRemove := httptest.NewRequest("POST", "/api/minecraft/1/mods/remove", strings.NewReader(`{"slug":"ferrite-core"}`))
	reqRemove.Header.Set("Content-Type", "application/json")
	respRemove, err := app.Test(reqRemove)
	if err != nil || respRemove.StatusCode != fiber.StatusOK {
		t.Fatalf("mod remove failed: %v, status: %d", err, respRemove.StatusCode)
	}
}

func TestMCConfigAndInstanceConfigEndpoints(t *testing.T) {
	h, st, _, mgr := setupTestInstancesHandler(t)
	defer st.Close()

	_, err := mgr.CreateInstance(context.Background(), domain.Instance{
		Name:      "Ducktopia",
		MCVersion: "1.21.1",
		Loader:    domain.LoaderNeoForge,
		Tier:      domain.TierMedium,
		State:     domain.StateStopped,
	}, "jei\n")
	if err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	h.Register(app)

	// 1. Global config pages
	reqList := httptest.NewRequest("GET", "/minecraft/configs", nil)
	respList, _ := app.Test(reqList)
	if respList.StatusCode != fiber.StatusTemporaryRedirect {
		t.Errorf("expected 307 for configs redirect, got: %d", respList.StatusCode)
	}

	app.Get("/test/configs/page", h.MCConfigsPage)
	respPage, _ := app.Test(httptest.NewRequest("GET", "/test/configs/page", nil))
	if respPage.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for MCConfigsPage, got: %d", respPage.StatusCode)
	}

	reqNew := httptest.NewRequest("GET", "/minecraft/configs/new", nil)
	respNew, _ := app.Test(reqNew)
	if respNew.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for configs new, got: %d", respNew.StatusCode)
	}

	reqEdit := httptest.NewRequest("GET", "/minecraft/configs/edit?f=server.properties", nil)
	respEdit, _ := app.Test(reqEdit)
	if respEdit.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for configs edit, got: %d", respEdit.StatusCode)
	}

	// Invalid config edit file name
	reqBadEdit := httptest.NewRequest("GET", "/minecraft/configs/edit?f=invalid*name", nil)
	respBadEdit, _ := app.Test(reqBadEdit)
	if respBadEdit.StatusCode != fiber.StatusSeeOther {
		t.Errorf("expected 303 for bad config edit, got: %d", respBadEdit.StatusCode)
	}

	// 2. Save and delete global config
	reqBadSave := httptest.NewRequest("POST", "/minecraft/configs/save", strings.NewReader(`file=invalid*name&content=foo`))
	reqBadSave.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respBadSave, _ := app.Test(reqBadSave)
	if respBadSave.StatusCode != fiber.StatusSeeOther {
		t.Errorf("expected 303 for bad save, got: %d", respBadSave.StatusCode)
	}

	reqSave := httptest.NewRequest("POST", "/minecraft/configs/save", strings.NewReader(`file=server.properties&content=pvp=false`))
	reqSave.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respSave, _ := app.Test(reqSave)
	if respSave.StatusCode != fiber.StatusSeeOther {
		t.Errorf("expected 303 see other, got: %d", respSave.StatusCode)
	}

	// Save global config unchanged
	reqSaveSame := httptest.NewRequest("POST", "/minecraft/configs/save", strings.NewReader(`file=server.properties&content=pvp=false`))
	reqSaveSame.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respSaveSame, _ := app.Test(reqSaveSame)
	if respSaveSame.StatusCode != fiber.StatusSeeOther {
		t.Errorf("expected 303 see other for unchanged, got: %d", respSaveSame.StatusCode)
	}

	reqBadDel := httptest.NewRequest("POST", "/minecraft/configs/delete", strings.NewReader(`file=invalid*name`))
	reqBadDel.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respBadDel, _ := app.Test(reqBadDel)
	if respBadDel.StatusCode != fiber.StatusSeeOther {
		t.Errorf("expected 303 for bad delete, got: %d", respBadDel.StatusCode)
	}

	reqDel := httptest.NewRequest("POST", "/minecraft/configs/delete", strings.NewReader(`file=server.properties`))
	reqDel.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respDel, _ := app.Test(reqDel)
	if respDel.StatusCode != fiber.StatusSeeOther {
		t.Errorf("expected 303 see other, got: %d", respDel.StatusCode)
	}

	// Delete again (not present)
	reqDelAgain := httptest.NewRequest("POST", "/minecraft/configs/delete", strings.NewReader(`file=server.properties`))
	reqDelAgain.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respDelAgain, _ := app.Test(reqDelAgain)
	if respDelAgain.StatusCode != fiber.StatusSeeOther {
		t.Errorf("expected 303 see other, got: %d", respDelAgain.StatusCode)
	}

	// 3. Per-instance config file get/save/delete
	// Get missing f parameter
	reqNoF := httptest.NewRequest("GET", "/api/minecraft/1/configs/file", nil)
	respNoF, _ := app.Test(reqNoF)
	if respNoF.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for missing f, got: %d", respNoF.StatusCode)
	}

	reqInstGet := httptest.NewRequest("GET", "/api/minecraft/1/configs/file?f=server.properties", nil)
	respInstGet, _ := app.Test(reqInstGet)
	if respInstGet.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for instance config get, got: %d", respInstGet.StatusCode)
	}

	// Save with invalid file name
	reqBadInstSave := httptest.NewRequest("POST", "/api/minecraft/1/configs/save", strings.NewReader(`{"file":"bad*name","content":"foo"}`))
	reqBadInstSave.Header.Set("Content-Type", "application/json")
	respBadInstSave, _ := app.Test(reqBadInstSave)
	bodyBadInstSave, _ := io.ReadAll(respBadInstSave.Body)
	if !strings.Contains(string(bodyBadInstSave), "Invalid config file name") {
		t.Errorf("expected invalid file name toast, got: %s", string(bodyBadInstSave))
	}

	// Save valid
	reqInstSave := httptest.NewRequest("POST", "/api/minecraft/1/configs/save", strings.NewReader(`{"file":"server.properties","content":"motd=Test"}`))
	reqInstSave.Header.Set("Content-Type", "application/json")
	respInstSave, _ := app.Test(reqInstSave)
	bodyInstSave, _ := io.ReadAll(respInstSave.Body)
	if !strings.Contains(string(bodyInstSave), "Saved server.properties") {
		t.Errorf("expected saved toast, got: %s", string(bodyInstSave))
	}

	// Save unchanged
	reqInstSaveSame := httptest.NewRequest("POST", "/api/minecraft/1/configs/save", strings.NewReader(`{"file":"server.properties","content":"motd=Test"}`))
	reqInstSaveSame.Header.Set("Content-Type", "application/json")
	respInstSaveSame, _ := app.Test(reqInstSaveSame)
	bodyInstSaveSame, _ := io.ReadAll(respInstSaveSame.Body)
	if !strings.Contains(string(bodyInstSaveSame), "server.properties is unchanged") {
		t.Errorf("expected unchanged toast, got: %s", string(bodyInstSaveSame))
	}

	// Delete missing file
	reqEmptyDel := httptest.NewRequest("POST", "/api/minecraft/1/configs/delete", strings.NewReader(`{}`))
	reqEmptyDel.Header.Set("Content-Type", "application/json")
	respEmptyDel, _ := app.Test(reqEmptyDel)
	bodyEmptyDel, _ := io.ReadAll(respEmptyDel.Body)
	if !strings.Contains(string(bodyEmptyDel), "File name required") {
		t.Errorf("expected file name required toast, got: %s", string(bodyEmptyDel))
	}


	// Delete valid
	reqInstDelete := httptest.NewRequest("POST", "/api/minecraft/1/configs/delete", strings.NewReader(`{"file":"server.properties"}`))
	reqInstDelete.Header.Set("Content-Type", "application/json")
	respInstDelete, _ := app.Test(reqInstDelete)
	bodyInstDelete, _ := io.ReadAll(respInstDelete.Body)
	if !strings.Contains(string(bodyInstDelete), "Deleted server.properties") {
		t.Errorf("expected deleted toast, got: %s", string(bodyInstDelete))
	}

	// Delete again
	reqInstDelAgain := httptest.NewRequest("POST", "/api/minecraft/1/configs/delete", strings.NewReader(`{"file":"server.properties"}`))
	reqInstDelAgain.Header.Set("Content-Type", "application/json")
	respInstDelAgain, _ := app.Test(reqInstDelAgain)
	bodyInstDelAgain, _ := io.ReadAll(respInstDelAgain.Body)
	if !strings.Contains(string(bodyInstDelAgain), "server.properties was not present") {
		t.Errorf("expected not present toast, got: %s", string(bodyInstDelAgain))
	}

	// 4. Legacy redirects
	reqModsRedir := httptest.NewRequest("GET", "/minecraft/mods", nil)
	respModsRedir, _ := app.Test(reqModsRedir)
	if respModsRedir.StatusCode != fiber.StatusTemporaryRedirect {
		t.Errorf("expected 307 for mods redirect, got: %d", respModsRedir.StatusCode)
	}

	// 5. Unconfigured MCInstances
	hUnconf := New(Config{})
	appUnconf := fiber.New()
	hUnconf.Register(appUnconf)

	respUnconfGet, _ := appUnconf.Test(httptest.NewRequest("GET", "/api/minecraft/1/configs/file?f=server.properties", nil))
	if respUnconfGet.StatusCode != fiber.StatusServiceUnavailable {
		t.Errorf("expected 503, got: %d", respUnconfGet.StatusCode)
	}

	respUnconfSave, _ := appUnconf.Test(httptest.NewRequest("POST", "/api/minecraft/1/configs/save", nil))
	bodyUnconfSave, _ := io.ReadAll(respUnconfSave.Body)
	if !strings.Contains(string(bodyUnconfSave), "Instance manager unconfigured") {
		t.Errorf("expected unconfigured toast, got: %s", string(bodyUnconfSave))
	}

	respUnconfDel, _ := appUnconf.Test(httptest.NewRequest("POST", "/api/minecraft/1/configs/delete", nil))
	bodyUnconfDel, _ := io.ReadAll(respUnconfDel.Body)
	if !strings.Contains(string(bodyUnconfDel), "Instance manager unconfigured") {
		t.Errorf("expected unconfigured toast, got: %s", string(bodyUnconfDel))
	}

	respUnconfSaveGlobal, _ := appUnconf.Test(httptest.NewRequest("POST", "/minecraft/configs/save", nil))
	if respUnconfSaveGlobal.StatusCode != fiber.StatusSeeOther {
		t.Errorf("expected 303, got: %d", respUnconfSaveGlobal.StatusCode)
	}

	respUnconfDelGlobal, _ := appUnconf.Test(httptest.NewRequest("POST", "/minecraft/configs/delete", nil))
	if respUnconfDelGlobal.StatusCode != fiber.StatusSeeOther {
		t.Errorf("expected 303, got: %d", respUnconfDelGlobal.StatusCode)
	}

	respUnconfRedir, _ := appUnconf.Test(httptest.NewRequest("GET", "/minecraft/mods", nil))
	if respUnconfRedir.StatusCode != fiber.StatusTemporaryRedirect {
		t.Errorf("expected 307, got: %d", respUnconfRedir.StatusCode)
	}
}

func TestMCDashboardInstanceActions(t *testing.T) {
	h, st, _, mgr := setupTestInstancesHandler(t)
	defer st.Close()

	_, err := mgr.CreateInstance(context.Background(), domain.Instance{
		Name:      "Ducktopia",
		MCVersion: "1.21.1",
		Loader:    domain.LoaderNeoForge,
		Tier:      domain.TierMedium,
		State:     domain.StateStopped,
	}, "jei\n")
	if err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	h.Register(app)

	// 1. Restart
	reqRestart := httptest.NewRequest("POST", "/api/minecraft/instances/1/restart", nil)
	respRestart, _ := app.Test(reqRestart)
	if respRestart.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for restart, got: %d", respRestart.StatusCode)
	}

	// 2. Start
	reqStart := httptest.NewRequest("POST", "/api/minecraft/instances/1/start", nil)
	respStart, _ := app.Test(reqStart)
	if respStart.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for start, got: %d", respStart.StatusCode)
	}

	// 3. Stop
	reqStop := httptest.NewRequest("POST", "/api/minecraft/instances/1/stop", nil)
	respStop, _ := app.Test(reqStop)
	if respStop.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for stop, got: %d", respStop.StatusCode)
	}

	// 4. Create Instance via API
	reqCreate := httptest.NewRequest("POST", "/api/minecraft/instances", strings.NewReader(`{"name":"Survival World","tier":"small","mc_version":"1.21.1","loader":"neoforge"}`))
	reqCreate.Header.Set("Content-Type", "application/json")
	respCreate, err := app.Test(reqCreate)
	if err != nil || respCreate.StatusCode != fiber.StatusOK {
		t.Errorf("create instance via API failed: %v, status: %d", err, respCreate.StatusCode)
	}

	// 5. Create Vanilla Instance
	reqVanilla := httptest.NewRequest("POST", "/api/minecraft/instances", strings.NewReader("name=VanillaWorld&loader=vanilla&tier=small"))
	reqVanilla.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respVanilla, _ := app.Test(reqVanilla)
	if respVanilla.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for vanilla create, got: %d", respVanilla.StatusCode)
	}

	// 6. Create Empty Name
	reqEmptyName := httptest.NewRequest("POST", "/api/minecraft/instances", strings.NewReader("name=&loader=neoforge"))
	reqEmptyName.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respEmptyName, _ := app.Test(reqEmptyName)
	bodyEmptyName, _ := io.ReadAll(respEmptyName.Body)
	if !strings.Contains(string(bodyEmptyName), "World name is required") {
		t.Errorf("expected world name required toast, got: %s", string(bodyEmptyName))
	}

	// 7. Delete Instance
	reqDelete := httptest.NewRequest("DELETE", "/api/minecraft/instances/1", nil)
	respDelete, _ := app.Test(reqDelete)
	if respDelete.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for delete, got: %d", respDelete.StatusCode)
	}

	// 8. Invalid numbers
	for _, badEndpoint := range []struct {
		method string
		path   string
	}{
		{"POST", "/api/minecraft/instances/bad/start"},
		{"POST", "/api/minecraft/instances/bad/stop"},
		{"POST", "/api/minecraft/instances/bad/restart"},
		{"DELETE", "/api/minecraft/instances/bad"},
	} {
		r := httptest.NewRequest(badEndpoint.method, badEndpoint.path, nil)
		resp, _ := app.Test(r)
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "Invalid instance number") {
			t.Errorf("%s %s expected invalid instance number, got: %s", badEndpoint.method, badEndpoint.path, string(body))
		}
	}

	// 9. Unconfigured instance manager
	hUnconf := New(Config{})
	appUnconf := fiber.New()
	hUnconf.Register(appUnconf)

	respUnconfDash, _ := appUnconf.Test(httptest.NewRequest("GET", "/minecraft", nil))
	if respUnconfDash.StatusCode != fiber.StatusServiceUnavailable {
		t.Errorf("expected 503 for unconf dashboard, got: %d", respUnconfDash.StatusCode)
	}

	for _, ep := range []struct {
		method string
		path   string
	}{
		{"POST", "/api/minecraft/instances"},
		{"POST", "/api/minecraft/instances/1/start"},
		{"POST", "/api/minecraft/instances/1/stop"},
		{"POST", "/api/minecraft/instances/1/restart"},
		{"DELETE", "/api/minecraft/instances/1"},
	} {
		r := httptest.NewRequest(ep.method, ep.path, nil)
		resp, _ := appUnconf.Test(r)
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "Instance manager not configured") {
			t.Errorf("expected unconf toast for %s %s, got: %s", ep.method, ep.path, string(body))
		}
	}
}

