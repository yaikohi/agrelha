package valheim

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/instances"
	"agrelha/internal/app/modupdates"
	"agrelha/internal/domain"
	valheimmanifests "agrelha/internal/infra/manifests/valheim"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
)

type mockRuntime struct {
	status     ports.Status
	startErr   error
	stopErr    error
	restartErr error
}

func (m *mockRuntime) Start(ctx context.Context, ref ports.ServerRef) error   { return m.startErr }
func (m *mockRuntime) Stop(ctx context.Context, ref ports.ServerRef) error    { return m.stopErr }
func (m *mockRuntime) Restart(ctx context.Context, ref ports.ServerRef) error { return m.restartErr }
func (m *mockRuntime) Status(ctx context.Context, ref ports.ServerRef) (ports.Status, error) {
	return m.status, nil
}
func (m *mockRuntime) Metrics(ctx context.Context, ref ports.ServerRef) (ports.Metrics, error) {
	return ports.Metrics{}, nil
}
func (m *mockRuntime) Logs(ctx context.Context, ref ports.ServerRef, opts ports.LogOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockRuntime) WatchAvailability(ctx context.Context, ref ports.ServerRef, timeout time.Duration) error {
	return nil
}

type mockPackageCatalog struct {
	results   []domain.ModSearchResult
	searchErr error
	readmeErr error
	latestErr error
	treeErr   error
}

func (m *mockPackageCatalog) Get(fullName string) (domain.ModSearchResult, bool) {
	for _, r := range m.results {
		if fmt.Sprintf("%s/%s", r.Owner, r.Name) == fullName || fmt.Sprintf("%s-%s", r.Owner, r.Name) == fullName || r.Name == fullName {
			return r, true
		}
	}
	return domain.ModSearchResult{}, false
}
func (m *mockPackageCatalog) Search(ctx context.Context, query string, limit int) ([]domain.ModSearchResult, error) {
	if m.searchErr != nil {
		return nil, m.searchErr
	}
	return m.results, nil
}
func (m *mockPackageCatalog) Ready() bool { return true }
func (m *mockPackageCatalog) LatestVersion(ctx context.Context, ns, name string) (string, []string, error) {
	if m.latestErr != nil {
		return "", nil, m.latestErr
	}
	return "1.2.0", []string{"denikson-BepInExPack_Valheim-5.4.2202", "Author-OtherMod-1.0.0"}, nil
}
func (m *mockPackageCatalog) Readme(ctx context.Context, ns, name, version string) (string, error) {
	if m.readmeErr != nil {
		return "", m.readmeErr
	}
	return "# " + name + "\n\nAwesome Valheim mod by " + ns, nil
}
func (m *mockPackageCatalog) ResolveTree(ctx context.Context, ns, name string) ([]string, error) {
	if m.treeErr != nil {
		return nil, m.treeErr
	}
	return nil, nil
}

type memStateStore struct {
	mu       sync.Mutex
	docs     map[string]*ports.Document
	patchErr error
	putErr   error
	delErr   error
}

func newMemStateStore() *memStateStore {
	return &memStateStore{docs: make(map[string]*ports.Document)}
}

func (m *memStateStore) Get(ctx context.Context, path string) (ports.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, ok := m.docs[path]
	if !ok {
		return ports.Document{Data: make(map[string]string)}, nil
	}
	return *doc, nil
}

func (m *memStateStore) Put(ctx context.Context, path string, doc ports.Document, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.putErr != nil {
		return m.putErr
	}
	m.docs[path] = &doc
	return nil
}

func (m *memStateStore) Patch(ctx context.Context, path string, msg string, mutate func(doc *ports.Document) (bool, error)) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.patchErr != nil {
		return false, m.patchErr
	}
	doc, ok := m.docs[path]
	if !ok {
		doc = &ports.Document{Data: make(map[string]string)}
		m.docs[path] = doc
	}
	return mutate(doc)
}

func (m *memStateStore) Delete(ctx context.Context, path string, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.delErr != nil {
		return m.delErr
	}
	delete(m.docs, path)
	return nil
}

func (m *memStateStore) PutTree(ctx context.Context, dirPath string, docs map[string]ports.Document, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.putErr != nil {
		return m.putErr
	}
	for fname, doc := range docs {
		rel := filepath.Join(dirPath, fname)
		d := doc
		if d.Data == nil && len(d.Raw) > 0 {
			d.Data = make(map[string]string)
			rawStr := string(d.Raw)
			if idx := strings.Index(rawStr, "mods.txt: |"); idx != -1 {
				lines := strings.Split(rawStr[idx:], "\n")
				var modLines []string
				for _, line := range lines[1:] {
					if after, ok := strings.CutPrefix(line, "    "); ok {
						modLines = append(modLines, after)
					} else if strings.TrimSpace(line) != "" {
						break
					}
				}
				d.Data["mods.txt"] = strings.Join(modLines, "\n")
			}
		}
		m.docs[rel] = &d
	}
	return nil
}

func setupTestValheimHandlerFull(t *testing.T) (*Handler, *store.Store, *instances.InstanceManager, *mockRuntime, *memStateStore, *mockPackageCatalog, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "valheim-test.db"))
	if err != nil {
		t.Fatal(err)
	}

	rt := &mockRuntime{
		status: ports.Status{Lifecycle: ports.LifecycleRunning, Available: true},
	}

	bDir := t.TempDir()
	mockState := newMemStateStore()
	mgr := instances.NewInstanceManager(
		store.NewValheimInstanceRepo(st), mockState, rt,
		16, 4, 2, "manifests/valheim", "192.168.20.224",
		valheimmanifests.New("ykhi.xyz/gameserver=true", "valheim"),
		"valheim",
		instances.WithGameID(domain.GameValheim),
		instances.WithBackupsDir(bDir),
		instances.WithConfigsReader(func(ctx context.Context, num int) (map[string]string, error) {
			path := fmt.Sprintf("manifests/valheim/instance-%02d/configs.yaml", num)
			doc, _ := mockState.Get(ctx, path)
			if doc.Data == nil {
				return make(map[string]string), nil
			}
			return doc.Data, nil
		}),
		instances.WithModsReader(func(ctx context.Context, num int) ([]string, error) {
			path := fmt.Sprintf("manifests/valheim/instance-%02d/mods.yaml", num)
			doc, _ := mockState.Get(ctx, path)
			var lines []string
			for line := range strings.SplitSeq(doc.Data["mods.txt"], "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					lines = append(lines, line)
				}
			}
			return lines, nil
		}),
	)

	mockTS := &mockPackageCatalog{
		results: []domain.ModSearchResult{
			{
				Owner:       "Smoothbrain",
				Name:        "Mining",
				Version:     "1.2.0",
				Description: "Enhanced mining progression and skills",
			},
		},
	}

	h := New(Config{
		ValheimInstances: mgr,
		TS:               mockTS,
	})

	return h, st, mgr, rt, mockState, mockTS, bDir
}

func setupTestValheimHandler(t *testing.T) (*Handler, *store.Store, *instances.InstanceManager) {
	h, st, mgr, _, _, _, _ := setupTestValheimHandlerFull(t)
	return h, st, mgr
}

func TestValheimDashboard(t *testing.T) {
	h, st, mgr := setupTestValheimHandler(t)
	defer st.Close()

	ctx := context.Background()
	_, err := mgr.CreateInstance(ctx, domain.Instance{
		GameID: domain.GameValheim,
		Name:   "Odin's World",
		Tier:   domain.TierMedium,
	}, "", "tester")
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	app := fiber.New()
	h.Register(app)

	req := httptest.NewRequest("GET", "/valheim", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("GET /valheim failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
}

func TestValheimLifecycleEndpoints(t *testing.T) {
	h, st, mgr := setupTestValheimHandler(t)
	defer st.Close()

	ctx := context.Background()
	inst, err := mgr.CreateInstance(ctx, domain.Instance{
		GameID: domain.GameValheim,
		Name:   "Viking Realm",
		Tier:   domain.TierSmall,
	}, "", "tester")
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	app := fiber.New()
	h.Register(app)

	// Test Start
	req := httptest.NewRequest("POST", "/api/valheim/instances/1/start", nil)
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("start instance failed: %v, status: %d", err, resp.StatusCode)
	}

	// Test Restart
	req = httptest.NewRequest("POST", "/api/valheim/instances/1/restart", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("restart instance failed: %v, status: %d", err, resp.StatusCode)
	}

	// Test Stop
	req = httptest.NewRequest("POST", "/api/valheim/instances/1/stop", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("stop instance failed: %v, status: %d", err, resp.StatusCode)
	}

	_ = inst
}

func TestValheimInstancePage(t *testing.T) {
	h, st, mgr := setupTestValheimHandler(t)
	defer st.Close()

	ctx := context.Background()
	_, err := mgr.CreateInstance(ctx, domain.Instance{
		GameID:   domain.GameValheim,
		Name:     "Midgard",
		Password: "secretpassword",
		Tier:     domain.TierLarge,
	}, "denikson-BepInExPack_Valheim\n", "tester")
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	app := fiber.New()
	h.Register(app)

	for _, tab := range []string{"overview", "mods", "configs", "settings"} {
		req := httptest.NewRequest("GET", "/valheim/1/"+tab, nil)
		resp, err := app.Test(req)
		if err != nil || resp.StatusCode != fiber.StatusOK {
			t.Fatalf("GET /valheim/1/%s failed: %v, code: %d", tab, err, resp.StatusCode)
		}
	}
}

func TestValheimWizardCreate(t *testing.T) {
	h, st, _ := setupTestValheimHandler(t)
	defer st.Close()

	app := fiber.New()
	h.Register(app)

	body := `{"name":"Asgard","password":"valheimpass","seed":"odinseed","tier":"medium","source":"scratch","raw_mods":"ValheimModding-Jotunn"}`
	req := httptest.NewRequest("POST", "/api/valheim/wizard/create", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("wizard create failed: %v, status: %d", err, resp.StatusCode)
	}
}

func TestValheimLegacyRedirects(t *testing.T) {
	h, st, mgr := setupTestValheimHandler(t)
	defer st.Close()

	ctx := context.Background()
	_, _ = mgr.CreateInstance(ctx, domain.Instance{
		GameID: domain.GameValheim,
		Name:   "Valheim Default",
	}, "", "tester")

	app := fiber.New()
	h.Register(app)

	req := httptest.NewRequest("GET", "/mods", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusTemporaryRedirect {
		t.Errorf("expected 307 redirect for /mods, got %d", resp.StatusCode)
	}

	req = httptest.NewRequest("GET", "/configs", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusTemporaryRedirect {
		t.Errorf("expected 307 redirect for /configs, got %d", resp.StatusCode)
	}
}

func TestValheimInstanceModsSearchAndInstall(t *testing.T) {
	h, st, mgr := setupTestValheimHandler(t)
	defer st.Close()

	ctx := context.Background()
	_, _ = mgr.CreateInstance(ctx, domain.Instance{
		GameID: domain.GameValheim,
		Number: 1,
		Name:   "Valheim Default",
	}, "", "tester")

	app := fiber.New()
	h.Register(app)

	// 1. Search for "mining"
	searchBody := `{"modQuery":"mining"}`
	reqSearch := httptest.NewRequest("POST", "/api/valheim/1/mods/search", strings.NewReader(searchBody))
	reqSearch.Header.Set("Content-Type", "application/json")
	respSearch, err := app.Test(reqSearch)
	if err != nil || respSearch.StatusCode != fiber.StatusOK {
		t.Fatalf("search request failed: %v, status: %d", err, respSearch.StatusCode)
	}
	bodyBytes, _ := io.ReadAll(respSearch.Body)
	bodyStr := string(bodyBytes)
	if !strings.Contains(bodyStr, "Smoothbrain-Mining") {
		t.Errorf("expected search results to contain 'Smoothbrain-Mining', got %s", bodyStr)
	}

	// 2. Install "Smoothbrain-Mining" with JSON body
	installBody := `{"slug":"Smoothbrain-Mining"}`
	reqInstall := httptest.NewRequest("POST", "/api/valheim/1/mods/install", strings.NewReader(installBody))
	reqInstall.Header.Set("Content-Type", "application/json")
	respInstall, err := app.Test(reqInstall)
	if err != nil || respInstall.StatusCode != fiber.StatusOK {
		t.Fatalf("install request failed: %v, status: %d", err, respInstall.StatusCode)
	}
	installBytes, _ := io.ReadAll(respInstall.Body)
	installStr := string(installBytes)
	if !strings.Contains(installStr, "Installed Smoothbrain-Mining") {
		t.Errorf("expected install response to contain success toast, got: %s", installStr)
	}

	// Verify it was installed in instance repo/store
	installed, err := mgr.GetInstalledMods(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	found := slices.Contains(installed, "Smoothbrain-Mining")
	if !found {
		t.Errorf("expected Smoothbrain-Mining to be in installed mods: %v", installed)
	}

	// 3. Remove "Smoothbrain-Mining" with JSON body
	removeBody := `{"slug":"Smoothbrain-Mining"}`
	reqRemove := httptest.NewRequest("POST", "/api/valheim/1/mods/remove", strings.NewReader(removeBody))
	reqRemove.Header.Set("Content-Type", "application/json")
	respRemove, err := app.Test(reqRemove)
	if err != nil || respRemove.StatusCode != fiber.StatusOK {
		t.Fatalf("remove request failed: %v, status: %d", err, respRemove.StatusCode)
	}
	removeBytes, _ := io.ReadAll(respRemove.Body)
	removeStr := string(removeBytes)
	if !strings.Contains(removeStr, "Removed Smoothbrain-Mining") {
		t.Errorf("expected remove response to contain toast, got: %s", removeStr)
	}

	// 4. Install via query parameter (fallback test)
	reqInstallQuery := httptest.NewRequest("POST", "/api/valheim/1/mods/install?slug=Smoothbrain-Mining", nil)
	respInstallQuery, err := app.Test(reqInstallQuery)
	if err != nil || respInstallQuery.StatusCode != fiber.StatusOK {
		t.Fatalf("query install failed: %v, status: %d", err, respInstallQuery.StatusCode)
	}
	installQueryBytes, _ := io.ReadAll(respInstallQuery.Body)
	if !strings.Contains(string(installQueryBytes), "Installed Smoothbrain-Mining") {
		t.Errorf("expected query install response to contain toast, got: %s", string(installQueryBytes))
	}

	// 5. Remove via query parameter (fallback test)
	reqRemoveQuery := httptest.NewRequest("POST", "/api/valheim/1/mods/remove?slug=Smoothbrain-Mining", nil)
	respRemoveQuery, err := app.Test(reqRemoveQuery)
	if err != nil || respRemoveQuery.StatusCode != fiber.StatusOK {
		t.Fatalf("query remove failed: %v, status: %d", err, respRemoveQuery.StatusCode)
	}
	removeQueryBytes, _ := io.ReadAll(respRemoveQuery.Body)
	if !strings.Contains(string(removeQueryBytes), "Removed Smoothbrain-Mining") {
		t.Errorf("expected query remove response to contain toast, got: %s", string(removeQueryBytes))
	}
}

func TestValheimModDetail(t *testing.T) {
	h, st, mgr := setupTestValheimHandler(t)
	defer st.Close()

	ctx := context.Background()
	_, _ = mgr.CreateInstance(ctx, domain.Instance{
		GameID: domain.GameValheim,
		Number: 1,
		Name:   "Valheim Detail",
	}, "", "tester")

	app := fiber.New()
	h.Register(app)

	req := httptest.NewRequest("GET", "/api/valheim/1/mods/detail?slug=Smoothbrain-Mining", nil)
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("detail request failed: %v, status: %d", err, resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)
	if !strings.Contains(bodyStr, "showModDetail") {
		t.Errorf("expected detail response to patch showModDetail, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "Awesome Valheim mod") {
		t.Errorf("expected rendered README in detail response, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "Install Mod") {
		t.Errorf("expected Install Mod button in detail response, got: %s", bodyStr)
	}

	// Test POST detail with JSON payload (avoiding query param length limits)
	reqPost := httptest.NewRequest("POST", "/api/valheim/1/mods/detail", strings.NewReader(`{"slug":"Smoothbrain-Mining"}`))
	reqPost.Header.Set("Content-Type", "application/json")
	respPost, err := app.Test(reqPost)
	if err != nil || respPost.StatusCode != fiber.StatusOK {
		t.Fatalf("POST detail request failed: %v, status: %d", err, respPost.StatusCode)
	}
	postBytes, _ := io.ReadAll(respPost.Body)
	if !strings.Contains(string(postBytes), "showModDetail") {
		t.Errorf("expected POST detail response to patch showModDetail, got: %s", string(postBytes))
	}
}

func TestValheimWizardModsSearch(t *testing.T) {
	h, st, _ := setupTestValheimHandler(t)
	defer st.Close()

	app := fiber.New()
	h.Register(app)

	// GET initial popular mods
	reqGet := httptest.NewRequest("GET", "/api/valheim/wizard/mods/search", nil)
	respGet, err := app.Test(reqGet)
	if err != nil || respGet.StatusCode != fiber.StatusOK {
		t.Fatalf("wizard search GET failed: %v, status: %d", err, respGet.StatusCode)
	}
	getBody, _ := io.ReadAll(respGet.Body)
	getStr := string(getBody)
	if !strings.Contains(getStr, "Smoothbrain-Mining") {
		t.Errorf("expected GET search to contain Smoothbrain-Mining, got: %s", getStr)
	}
	if !strings.Contains(getStr, "In Cart") {
		t.Errorf("expected GET search cards to contain In Cart badge markup, got: %s", getStr)
	}

	// POST search with query
	reqPost := httptest.NewRequest("POST", "/api/valheim/wizard/mods/search", strings.NewReader(`{"wizardModSearch":"mining"}`))
	reqPost.Header.Set("Content-Type", "application/json")
	respPost, err := app.Test(reqPost)
	if err != nil || respPost.StatusCode != fiber.StatusOK {
		t.Fatalf("wizard search POST failed: %v, status: %d", err, respPost.StatusCode)
	}
	postBody, _ := io.ReadAll(respPost.Body)
	postStr := string(postBody)
	if !strings.Contains(postStr, "Search Results for") {
		t.Errorf("expected POST search to contain search header, got: %s", postStr)
	}
}

func TestValheimWizardModDetail(t *testing.T) {
	h, st, _ := setupTestValheimHandler(t)
	defer st.Close()

	app := fiber.New()
	h.Register(app)

	req := httptest.NewRequest("GET", "/api/valheim/wizard/mods/detail?slug=Smoothbrain-Mining", nil)
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("wizard mod detail failed: %v, status: %d", err, resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)
	if !strings.Contains(bodyStr, "showWizardModDetail") {
		t.Errorf("expected wizard mod detail to patch showWizardModDetail signal, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "+ Add to Cart") {
		t.Errorf("expected Add to Cart button in wizard mod detail, got: %s", bodyStr)
	}

	// Test POST wizard detail with JSON payload
	reqPost := httptest.NewRequest("POST", "/api/valheim/wizard/mods/detail", strings.NewReader(`{"slug":"Smoothbrain-Mining"}`))
	reqPost.Header.Set("Content-Type", "application/json")
	respPost, err := app.Test(reqPost)
	if err != nil || respPost.StatusCode != fiber.StatusOK {
		t.Fatalf("POST wizard mod detail failed: %v, status: %d", err, respPost.StatusCode)
	}
	postBytes, _ := io.ReadAll(respPost.Body)
	if !strings.Contains(string(postBytes), "showWizardModDetail") {
		t.Errorf("expected POST wizard detail to patch showWizardModDetail signal, got: %s", string(postBytes))
	}
}

func TestValheimWizardCartSync(t *testing.T) {
	h, st, _ := setupTestValheimHandler(t)
	defer st.Close()

	app := fiber.New()
	h.Register(app)

	cartBody := `{"cart":["Smoothbrain-Mining","ValheimModding-Jotunn"]}`
	req := httptest.NewRequest("POST", "/api/valheim/wizard/cart/sync", strings.NewReader(cartBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("cart sync failed: %v, status: %d", err, resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)
	if !strings.Contains(bodyStr, "wizard-valheim-cart-items") {
		t.Errorf("expected cart items element target, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "Smoothbrain-Mining") || !strings.Contains(bodyStr, "ValheimModding-Jotunn") {
		t.Errorf("expected both mod slugs in cart chips, got: %s", bodyStr)
	}
}

func TestValheimWizardCreateWithCart(t *testing.T) {
	h, st, mgr := setupTestValheimHandler(t)
	defer st.Close()

	app := fiber.New()
	h.Register(app)

	createBody := `{"name":"Cart Realm","source":"scratch","tier":"medium","cart":["Smoothbrain-Mining"]}`
	req := httptest.NewRequest("POST", "/api/valheim/wizard/create", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("wizard create failed: %v, status: %d", err, resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)
	if !strings.Contains(bodyStr, "Created Valheim server") {
		t.Errorf("expected success toast, got: %s", bodyStr)
	}

	// Verify the instance installed mods
	ctx := context.Background()
	insts, err := mgr.ListInstances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(insts) == 0 {
		t.Fatal("expected at least one instance created")
	}

	installed, err := mgr.GetInstalledMods(ctx, insts[0].Number)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(installed, "Smoothbrain-Mining") {
		t.Errorf("expected Smoothbrain-Mining in installed mods, got: %v", installed)
	}
}

type mockValheimGame struct {
	ports.Game
	exportErr error
	bundle    domain.Bundle
}

func (m *mockValheimGame) ExportClientBundle(ctx context.Context, inst domain.Instance) (domain.Bundle, error) {
	if m.exportErr != nil {
		return domain.Bundle{}, m.exportErr
	}
	return m.bundle, nil
}

func TestValheimModUpdatesEndpoints(t *testing.T) {
	h, st, mgr := setupTestValheimHandler(t)
	defer st.Close()

	ctx := context.Background()
	_, err := mgr.CreateInstance(ctx, domain.Instance{
		GameID: domain.GameValheim,
		Number: 1,
		Name:   "Viking Realm",
		Source: domain.SourceModlist,
	}, "Smoothbrain/Mining/1.0.0\n", "tester")
	if err != nil {
		t.Fatal(err)
	}

	mockTS := &mockPackageCatalog{
		results: []domain.ModSearchResult{
			{Owner: "Smoothbrain", Name: "Mining", Version: "1.2.0"},
		},
	}
	h.cfg.ModUpdates = modupdates.New(mgr, mockTS, modupdates.WithGameID(domain.GameValheim), modupdates.WithRestorePoints(st))

	app := fiber.New()
	h.Register(app)

	// 1. Check updates
	reqCheck := httptest.NewRequest("POST", "/api/valheim/1/mods/updates/check", nil)
	respCheck, err := app.Test(reqCheck)
	if err != nil || respCheck.StatusCode != fiber.StatusOK {
		t.Fatalf("check failed: %v, status: %d", err, respCheck.StatusCode)
	}
	bodyCheck, _ := io.ReadAll(respCheck.Body)
	if !strings.Contains(string(bodyCheck), "mod-updates-panel") {
		t.Errorf("expected mod-updates-panel in response: %s", string(bodyCheck))
	}

	// 2. Apply updates (all: true)
	applyBody := `{"all":true}`
	reqApply := httptest.NewRequest("POST", "/api/valheim/1/mods/updates/apply", strings.NewReader(applyBody))
	reqApply.Header.Set("Content-Type", "application/json")
	respApply, err := app.Test(reqApply)
	if err != nil || respApply.StatusCode != fiber.StatusOK {
		t.Fatalf("apply failed: %v, status: %d", err, respApply.StatusCode)
	}

	// 3. Undo updates
	reqUndo := httptest.NewRequest("POST", "/api/valheim/1/mods/updates/undo", nil)
	respUndo, err := app.Test(reqUndo)
	if err != nil || respUndo.StatusCode != fiber.StatusOK {
		t.Fatalf("undo failed: %v, status: %d", err, respUndo.StatusCode)
	}
	bodyUndo, _ := io.ReadAll(respUndo.Body)
	if !strings.Contains(string(bodyUndo), "Reverted") {
		t.Errorf("expected Reverted in undo response: %s", string(bodyUndo))
	}

	// 4. Error cases
	// No mods selected
	reqEmptyApply := httptest.NewRequest("POST", "/api/valheim/1/mods/updates/apply", strings.NewReader(`{"all":false}`))
	reqEmptyApply.Header.Set("Content-Type", "application/json")
	respEmptyApply, _ := app.Test(reqEmptyApply)
	bodyEmpty, _ := io.ReadAll(respEmptyApply.Body)
	if !strings.Contains(string(bodyEmpty), "No mods selected") {
		t.Errorf("expected 'No mods selected' toast, got: %s", string(bodyEmpty))
	}

	// Invalid instance
	reqInvalid := httptest.NewRequest("POST", "/api/valheim/99/mods/updates/check", nil)
	respInvalid, _ := app.Test(reqInvalid)
	bodyInvalid, _ := io.ReadAll(respInvalid.Body)
	if !strings.Contains(string(bodyInvalid), "not found") {
		t.Errorf("expected not found, got: %s", string(bodyInvalid))
	}

	// Unconfigured
	h.cfg.ModUpdates = nil
	reqUnconf := httptest.NewRequest("POST", "/api/valheim/1/mods/updates/check", nil)
	respUnconf, _ := app.Test(reqUnconf)
	bodyUnconf, _ := io.ReadAll(respUnconf.Body)
	if !strings.Contains(string(bodyUnconf), "not configured") {
		t.Errorf("expected not configured, got: %s", string(bodyUnconf))
	}
}

func TestValheimDetailTabsAndActions(t *testing.T) {
	h, st, mgr := setupTestValheimHandler(t)
	defer st.Close()

	ctx := context.Background()
	inst, err := mgr.CreateInstance(ctx, domain.Instance{
		GameID:   domain.GameValheim,
		Name:     "Odin Realm",
		Password: "secret",
		Tier:     domain.TierMedium,
		State:    domain.StateRunning,
	}, "", "tester")
	if err != nil {
		t.Fatal(err)
	}

	// Configure LastIncident and ValheimGame
	h.cfg.LastIncident = func(ctx context.Context, number int) (*domain.Incident, error) {
		return &domain.Incident{
			ID:           1,
			GameID:       domain.GameValheim,
			Number:       1,
			At:           time.Now(),
			Reason:       "CrashLoopBackOff",
			RestartCount: 3,
		}, nil
	}
	h.cfg.ValheimGame = &mockValheimGame{
		bundle: domain.Bundle{
			Filename:    "odin-realm.r2z",
			ContentType: "application/zip",
			Data:        []byte("PKmockzip"),
		},
	}

	app := fiber.New()
	h.Register(app)

	// Test backups tab
	reqBackups := httptest.NewRequest("GET", "/valheim/1/backups", nil)
	respBackups, err := app.Test(reqBackups)
	if err != nil || respBackups.StatusCode != fiber.StatusOK {
		t.Errorf("backups tab failed: %v, status: %d", err, respBackups.StatusCode)
	}

	// Test settings save
	settingsBody := "name=Renamed+Odin&password=newsecret&tier=large"
	reqSave := httptest.NewRequest("POST", "/api/valheim/1/settings", strings.NewReader(settingsBody))
	reqSave.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respSave, err := app.Test(reqSave)
	if err != nil || respSave.StatusCode != fiber.StatusOK {
		t.Errorf("settings save failed: %v, status: %d", err, respSave.StatusCode)
	}
	saveBody, _ := io.ReadAll(respSave.Body)
	if !strings.Contains(string(saveBody), "saved successfully") {
		t.Errorf("expected saved successfully toast, got: %s", string(saveBody))
	}

	// Test settings save not found
	reqBadSave := httptest.NewRequest("POST", "/api/valheim/99/settings", strings.NewReader(settingsBody))
	reqBadSave.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respBadSave, _ := app.Test(reqBadSave)
	badSaveBody, _ := io.ReadAll(respBadSave.Body)
	if !strings.Contains(string(badSaveBody), "Failed to update settings") {
		t.Errorf("expected Failed to update settings toast, got: %s", string(badSaveBody))
	}

	// Test export
	reqExport := httptest.NewRequest("GET", "/api/valheim/1/mods/export", nil)
	respExport, err := app.Test(reqExport)
	if err != nil || respExport.StatusCode != fiber.StatusOK {
		t.Errorf("export failed: %v, status: %d", err, respExport.StatusCode)
	}
	if respExport.Header.Get("Content-Type") != "application/zip" {
		t.Errorf("expected application/zip, got: %s", respExport.Header.Get("Content-Type"))
	}

	// Test empty mod slug install & remove errors
	reqEmptyInstall := httptest.NewRequest("POST", "/api/valheim/1/mods/install", strings.NewReader(`{"slug":""}`))
	reqEmptyInstall.Header.Set("Content-Type", "application/json")
	respEmptyInstall, _ := app.Test(reqEmptyInstall)
	bodyEmptyInstall, _ := io.ReadAll(respEmptyInstall.Body)
	if !strings.Contains(string(bodyEmptyInstall), "Mod slug required") {
		t.Errorf("expected mod slug required error, got: %s", string(bodyEmptyInstall))
	}

	reqEmptyRemove := httptest.NewRequest("POST", "/api/valheim/1/mods/remove", strings.NewReader(`{"slug":""}`))
	reqEmptyRemove.Header.Set("Content-Type", "application/json")
	respEmptyRemove, _ := app.Test(reqEmptyRemove)
	bodyEmptyRemove, _ := io.ReadAll(respEmptyRemove.Body)
	if !strings.Contains(string(bodyEmptyRemove), "Mod slug required") {
		t.Errorf("expected mod slug required error, got: %s", string(bodyEmptyRemove))
	}

	_ = inst
}

func TestValheimWizardPageAndImport(t *testing.T) {
	h, st, _ := setupTestValheimHandler(t)
	defer st.Close()

	app := fiber.New()
	h.Register(app)

	// 1. GET /valheim/create
	reqPage := httptest.NewRequest("GET", "/valheim/create", nil)
	respPage, err := app.Test(reqPage)
	if err != nil || respPage.StatusCode != fiber.StatusOK {
		t.Fatalf("wizard page failed: %v, status: %d", err, respPage.StatusCode)
	}

	// 2. Wizard search with Accept: application/json
	reqJSONSearch := httptest.NewRequest("GET", "/api/valheim/wizard/mods/search?q=mining", nil)
	reqJSONSearch.Header.Set("Accept", "application/json")
	respJSONSearch, err := app.Test(reqJSONSearch)
	if err != nil || respJSONSearch.StatusCode != fiber.StatusOK {
		t.Fatalf("json search failed: %v, status: %d", err, respJSONSearch.StatusCode)
	}

	// 3. Valid import (.r2z profile with manifest.json)
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	manifestData := `{"name":"ValheimPack","dependencies":["Smoothbrain-Mining-1.2.0"]}`
	mf, err := zw.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = mf.Write([]byte(manifestData))
	_ = zw.Close()

	var bodyBuf bytes.Buffer
	mpw := multipart.NewWriter(&bodyBuf)
	part, err := mpw.CreateFormFile("file", "profile.r2z")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(zipBuf.Bytes())
	_ = mpw.Close()

	reqImport := httptest.NewRequest("POST", "/api/valheim/wizard/import", &bodyBuf)
	reqImport.Header.Set("Content-Type", mpw.FormDataContentType())
	respImport, err := app.Test(reqImport)
	if err != nil || respImport.StatusCode != fiber.StatusOK {
		t.Fatalf("import failed: %v, status: %d", err, respImport.StatusCode)
	}
	bodyImport, _ := io.ReadAll(respImport.Body)
	if !strings.Contains(string(bodyImport), "Imported 1 mods from profile!") {
		t.Errorf("expected success toast, got: %s", string(bodyImport))
	}

	// 4. Invalid import (missing file)
	reqBadImport := httptest.NewRequest("POST", "/api/valheim/wizard/import", nil)
	respBadImport, _ := app.Test(reqBadImport)
	bodyBadImport, _ := io.ReadAll(respBadImport.Body)
	if !strings.Contains(string(bodyBadImport), "File upload failed") {
		t.Errorf("expected upload failed toast, got: %s", string(bodyBadImport))
	}
}

func TestValheimConfigs(t *testing.T) {
	h, st, mgr := setupTestValheimHandler(t)
	defer st.Close()

	ctx := context.Background()
	_, err := mgr.CreateInstance(ctx, domain.Instance{
		GameID: domain.GameValheim,
		Name:   "Valheim Server",
		Tier:   domain.TierMedium,
	}, "", "tester")
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	app := fiber.New()
	h.Register(app)

	// 1. Get config - missing f parameter
	reqNoFile := httptest.NewRequest("GET", "/api/valheim/1/configs/file", nil)
	respNoFile, _ := app.Test(reqNoFile)
	if respNoFile.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for missing file name, got %d", respNoFile.StatusCode)
	}

	// 2. Save config - invalid filename
	reqBadSave := httptest.NewRequest("POST", "/api/valheim/1/configs/save", strings.NewReader("file=bad-name&content=foo"))
	reqBadSave.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respBadSave, _ := app.Test(reqBadSave)
	bodyBadSave, _ := io.ReadAll(respBadSave.Body)
	if !strings.Contains(string(bodyBadSave), "Invalid config file name") {
		t.Errorf("expected invalid config name toast, got %s", string(bodyBadSave))
	}

	// 3. Save config - success (new file)
	reqSave := httptest.NewRequest("POST", "/api/valheim/1/configs/save", strings.NewReader("file=server.cfg&content=difficulty=hard"))
	reqSave.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respSave, _ := app.Test(reqSave)
	bodySave, _ := io.ReadAll(respSave.Body)
	if !strings.Contains(string(bodySave), "Saved server.cfg") {
		t.Errorf("expected saved toast, got %s", string(bodySave))
	}

	// 4. Save config - unchanged
	reqSaveSame := httptest.NewRequest("POST", "/api/valheim/1/configs/save", strings.NewReader("file=server.cfg&content=difficulty=hard"))
	reqSaveSame.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respSaveSame, _ := app.Test(reqSaveSame)
	bodySaveSame, _ := io.ReadAll(respSaveSame.Body)
	if !strings.Contains(string(bodySaveSame), "server.cfg is unchanged") {
		t.Errorf("expected unchanged toast, got %s", string(bodySaveSame))
	}

	// 5. Get config - success
	reqGet := httptest.NewRequest("GET", "/api/valheim/1/configs/file?f=server.cfg", nil)
	respGet, _ := app.Test(reqGet)
	if respGet.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for get config, got %d", respGet.StatusCode)
	}
	bodyGet, _ := io.ReadAll(respGet.Body)
	if !strings.Contains(string(bodyGet), "difficulty=hard") {
		t.Errorf("expected file content in get response, got %s", string(bodyGet))
	}

	// 6. Delete config - invalid filename
	reqBadDelete := httptest.NewRequest("POST", "/api/valheim/1/configs/delete", strings.NewReader("file=bad-name"))
	reqBadDelete.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respBadDelete, _ := app.Test(reqBadDelete)
	bodyBadDelete, _ := io.ReadAll(respBadDelete.Body)
	if !strings.Contains(string(bodyBadDelete), "Invalid config file name") {
		t.Errorf("expected invalid filename error, got %s", string(bodyBadDelete))
	}

	// 7. Delete config - success
	reqDelete := httptest.NewRequest("POST", "/api/valheim/1/configs/delete", strings.NewReader("file=server.cfg"))
	reqDelete.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respDelete, _ := app.Test(reqDelete)
	bodyDelete, _ := io.ReadAll(respDelete.Body)
	if !strings.Contains(string(bodyDelete), "Deleted server.cfg") {
		t.Errorf("expected deleted toast, got %s", string(bodyDelete))
	}

	// 8. Delete config - not present
	reqDeleteAgain := httptest.NewRequest("POST", "/api/valheim/1/configs/delete", strings.NewReader("file=server.cfg"))
	reqDeleteAgain.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respDeleteAgain, _ := app.Test(reqDeleteAgain)
	bodyDeleteAgain, _ := io.ReadAll(respDeleteAgain.Body)
	if !strings.Contains(string(bodyDeleteAgain), "server.cfg was not present") {
		t.Errorf("expected not present toast, got %s", string(bodyDeleteAgain))
	}

	// 9. Unconfigured ValheimInstances
	hUnconf := New(Config{})
	appUnconf := fiber.New()
	hUnconf.Register(appUnconf)

	respUnconfGet, _ := appUnconf.Test(httptest.NewRequest("GET", "/api/valheim/1/configs/file?f=server.cfg", nil))
	if respUnconfGet.StatusCode != fiber.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", respUnconfGet.StatusCode)
	}

	respUnconfSave, _ := appUnconf.Test(httptest.NewRequest("POST", "/api/valheim/1/configs/save", nil))
	bodyUnconfSave, _ := io.ReadAll(respUnconfSave.Body)
	if !strings.Contains(string(bodyUnconfSave), "Valheim instance manager unconfigured") {
		t.Errorf("expected unconfigured toast, got %s", string(bodyUnconfSave))
	}

	respUnconfDel, _ := appUnconf.Test(httptest.NewRequest("POST", "/api/valheim/1/configs/delete", nil))
	bodyUnconfDel, _ := io.ReadAll(respUnconfDel.Body)
	if !strings.Contains(string(bodyUnconfDel), "Valheim instance manager unconfigured") {
		t.Errorf("expected unconfigured toast, got %s", string(bodyUnconfDel))
	}
}

