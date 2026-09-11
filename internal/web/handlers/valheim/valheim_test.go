package valheim

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	valheimmanifests "agrelha/internal/infra/manifests/valheim"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
)

type mockRuntime struct {
	status ports.Status
}

func (m *mockRuntime) Start(ctx context.Context, ref ports.ServerRef) error   { return nil }
func (m *mockRuntime) Stop(ctx context.Context, ref ports.ServerRef) error    { return nil }
func (m *mockRuntime) Restart(ctx context.Context, ref ports.ServerRef) error { return nil }
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
	results []domain.ModSearchResult
}

func (m *mockPackageCatalog) Get(fullName string) (domain.ModSearchResult, bool) {
	return domain.ModSearchResult{}, false
}
func (m *mockPackageCatalog) Search(ctx context.Context, query string, limit int) ([]domain.ModSearchResult, error) {
	return m.results, nil
}
func (m *mockPackageCatalog) Ready() bool { return true }
func (m *mockPackageCatalog) LatestVersion(ctx context.Context, ns, name string) (string, []string, error) {
	return "1.0.0", nil, nil
}
func (m *mockPackageCatalog) Readme(ctx context.Context, ns, name, version string) (string, error) {
	return "", nil
}
func (m *mockPackageCatalog) ResolveTree(ctx context.Context, ns, name string) ([]string, error) {
	return nil, nil
}

type memStateStore struct {
	mu   sync.Mutex
	docs map[string]*ports.Document
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
	m.docs[path] = &doc
	return nil
}

func (m *memStateStore) Patch(ctx context.Context, path string, msg string, mutate func(doc *ports.Document) (bool, error)) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	delete(m.docs, path)
	return nil
}

func (m *memStateStore) PutTree(ctx context.Context, dirPath string, docs map[string]ports.Document, msg string) error {
	return nil
}

func setupTestValheimHandler(t *testing.T) (*Handler, *store.Store, *instances.InstanceManager) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "valheim-test.db"))
	if err != nil {
		t.Fatal(err)
	}

	rt := &mockRuntime{
		status: ports.Status{Lifecycle: ports.LifecycleRunning, Available: true},
	}

	mockState := newMemStateStore()
	mgr := instances.NewInstanceManager(
		store.NewValheimInstanceRepo(st), mockState, rt,
		16, 4, 2, "manifests/valheim", "192.168.20.224",
		valheimmanifests.New("ykhi.xyz/gameserver=true", "valheim"),
		"valheim",
		instances.WithGameID(domain.GameValheim),
		instances.WithBackupsDir(t.TempDir()),
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
}
