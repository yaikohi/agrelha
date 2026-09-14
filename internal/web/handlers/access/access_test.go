package access

import (
	mcaccess "agrelha/internal/app/access"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"agrelha/internal/app/admins"
	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

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

func TestAccessHistoryPage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "access_test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	_ = st.RecordAudit("admin", "test-action", "test-target")

	h := New(Config{History: st})
	app := fiber.New()
	app.Get("/history", h.HistoryPage)

	req := httptest.NewRequest(http.MethodGet, "/history", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestAccessAdminsUnconfigured(t *testing.T) {
	h := New(Config{})
	app := fiber.New()
	h.Register(app)

	// 1. Admins page works even if k8s and store are nil
	reqPage := httptest.NewRequest(http.MethodGet, "/admins", nil)
	respPage, err := app.Test(reqPage)
	if err != nil {
		t.Fatal(err)
	}
	if respPage.StatusCode != http.StatusOK {
		t.Errorf("GET /admins status = %d, want 200", respPage.StatusCode)
	}

	// 2. Grant without admins configured redirects with flash
	reqGrant := httptest.NewRequest(http.MethodPost, "/admins/grant", strings.NewReader("steam_id=76561198000000001"))
	reqGrant.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respGrant, err := app.Test(reqGrant)
	if err != nil {
		t.Fatal(err)
	}
	if respGrant.StatusCode != http.StatusSeeOther {
		t.Errorf("POST /admins/grant status = %d, want 303", respGrant.StatusCode)
	}

	// 3. Revoke without admins configured redirects with flash
	reqRevoke := httptest.NewRequest(http.MethodPost, "/admins/revoke", strings.NewReader("steam_id=76561198000000001"))
	reqRevoke.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respRevoke, err := app.Test(reqRevoke)
	if err != nil {
		t.Fatal(err)
	}
	if respRevoke.StatusCode != http.StatusSeeOther {
		t.Errorf("POST /admins/revoke status = %d, want 303", respRevoke.StatusCode)
	}
}

func TestAccessMinecraftUnconfigured(t *testing.T) {
	h := New(Config{})
	app := fiber.New()
	h.Register(app)

	// 1. Minecraft access page renders 200
	reqPage := httptest.NewRequest(http.MethodGet, "/minecraft/access", nil)
	respPage, err := app.Test(reqPage)
	if err != nil {
		t.Fatal(err)
	}
	if respPage.StatusCode != http.StatusOK {
		t.Errorf("GET /minecraft/access status = %d, want 200", respPage.StatusCode)
	}

	// 2. JSON API returns 503 when MCAccess is nil
	apiEndpoints := []string{
		"/api/minecraft/access/op",
		"/api/minecraft/access/deop",
		"/api/minecraft/access/whitelist/add",
		"/api/minecraft/access/whitelist/remove",
		"/api/minecraft/access/whitelist/toggle",
	}
	for _, ep := range apiEndpoints {
		req := httptest.NewRequest(http.MethodPost, ep, strings.NewReader("username=testuser"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s: %v", ep, err)
		}
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s status = %d, want 503", ep, resp.StatusCode)
		}
	}

	// 3. HTML Form submission redirects with flash when MCAccess is nil
	for _, ep := range apiEndpoints {
		req := httptest.NewRequest(http.MethodPost, ep, strings.NewReader("username=testuser"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "text/html")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s: %v", ep, err)
		}
		if resp.StatusCode != http.StatusSeeOther {
			t.Errorf("%s HTML form status = %d, want 303", ep, resp.StatusCode)
		}
	}
}
func TestAccessConfiguredFlow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "access_test2.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	state := newMemStateStore()
	adm := admins.New(state, "valheim-admins.yaml", admins.WithAudit(st))
	mcAcc := mcaccess.NewAccessManager(state, "neoforge-access.yaml", nil, mcaccess.WithAudit(st))

	applied := false
	h := New(Config{
		Admins:     adm,
		MCAccess:   mcAcc,
		History:    st,
		Players:    st,
		StateStore: state,
		ApplyAfterSync: func(cmName, key string, want func(string) bool) {
			applied = true
		},
	})

	app := fiber.New()
	h.Register(app)

	// 1. Valheim grant
	reqGrant := httptest.NewRequest(http.MethodPost, "/admins/grant", strings.NewReader("steam_id=76561198012345678"))
	reqGrant.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respGrant, err := app.Test(reqGrant)
	if err != nil {
		t.Fatal(err)
	}
	if respGrant.StatusCode != http.StatusSeeOther {
		t.Errorf("grant status = %d, want 303", respGrant.StatusCode)
	}
	if !applied {
		t.Errorf("expected ApplyAfterSync to be called")
	}

	// 2. Valheim revoke
	reqRevoke := httptest.NewRequest(http.MethodPost, "/admins/revoke", strings.NewReader("steam_id=76561198012345678"))
	reqRevoke.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respRevoke, err := app.Test(reqRevoke)
	if err != nil {
		t.Fatal(err)
	}
	if respRevoke.StatusCode != http.StatusSeeOther {
		t.Errorf("revoke status = %d, want 303", respRevoke.StatusCode)
	}

	// 3. Minecraft GrantOp (JSON)
	reqOp := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/op", strings.NewReader("username=steve"))
	reqOp.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqOp.Header.Set("Accept", "application/json")
	respOp, err := app.Test(reqOp)
	if err != nil {
		t.Fatal(err)
	}
	if respOp.StatusCode != http.StatusOK {
		t.Errorf("op status = %d, want 200", respOp.StatusCode)
	}

	// 4. Minecraft RevokeOp (JSON)
	reqDeop := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/deop", strings.NewReader("username=steve"))
	reqDeop.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqDeop.Header.Set("Accept", "application/json")
	respDeop, err := app.Test(reqDeop)
	if err != nil {
		t.Fatal(err)
	}
	if respDeop.StatusCode != http.StatusOK {
		t.Errorf("deop status = %d, want 200", respDeop.StatusCode)
	}

	// 5. Minecraft AddWhitelist (JSON)
	reqWLAdd := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/whitelist/add", strings.NewReader("username=alex"))
	reqWLAdd.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqWLAdd.Header.Set("Accept", "application/json")
	respWLAdd, err := app.Test(reqWLAdd)
	if err != nil {
		t.Fatal(err)
	}
	if respWLAdd.StatusCode != http.StatusOK {
		t.Errorf("whitelist add status = %d, want 200", respWLAdd.StatusCode)
	}

	// 6. Minecraft RemoveWhitelist (JSON)
	reqWLRem := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/whitelist/remove", strings.NewReader("username=alex"))
	reqWLRem.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqWLRem.Header.Set("Accept", "application/json")
	respWLRem, err := app.Test(reqWLRem)
	if err != nil {
		t.Fatal(err)
	}
	if respWLRem.StatusCode != http.StatusOK {
		t.Errorf("whitelist remove status = %d, want 200", respWLRem.StatusCode)
	}
}

func TestAccessValheimPasswords(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "valheim_pass_test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	repo := store.NewValheimInstanceRepo(st)
	_ = repo.Upsert(domain.Instance{
		GameID:   domain.GameValheim,
		Number:   1,
		Name:     "Viking World",
		Password: "supersecretpass",
		State:    domain.StateRunning,
		LBIP:     "192.168.20.224",
	})
	_ = repo.Upsert(domain.Instance{
		GameID:   domain.GameValheim,
		Number:   2,
		Name:     "Public World",
		Password: "",
		State:    domain.StateStopped,
		LBIP:     "192.168.20.225",
	})

	mgr := instances.NewInstanceManager(
		repo, nil, nil, 16, 4, 2, "manifests/valheim", "192.168.20.224", nil, "valheim",
		instances.WithGameID(domain.GameValheim),
	)

	h := New(Config{
		ValheimInstances: mgr,
	})
	app := fiber.New()
	h.Register(app)

	req := httptest.NewRequest(http.MethodGet, "/admins", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /admins status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)

	if !strings.Contains(html, "Server Passwords") {
		t.Errorf("expected 'Server Passwords' card in HTML")
	}
	if !strings.Contains(html, "Viking World") {
		t.Errorf("expected 'Viking World' in HTML")
	}
	if !strings.Contains(html, "supersecretpass") {
		t.Errorf("expected 'supersecretpass' in HTML")
	}
	if !strings.Contains(html, "showPass1") {
		t.Errorf("expected showPass1 signal binding in HTML")
	}
	if !strings.Contains(html, "Public World") {
		t.Errorf("expected 'Public World' in HTML")
	}
	if !strings.Contains(html, "No password required (public access)") {
		t.Errorf("expected public access label for world 2 in HTML")
	}
}

type mockAccessConsole struct {
	responses map[string]string
	errs      map[string]error
	executed  []string
}

func (m *mockAccessConsole) Execute(cmd string) (string, error) {
	m.executed = append(m.executed, cmd)
	if err, ok := m.errs[cmd]; ok && err != nil {
		return "", err
	}
	if resp, ok := m.responses[cmd]; ok {
		return resp, nil
	}
	return "ok", nil
}

func TestMCAccessHTMLFormsAndErrors(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "access_mc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	stateStore := newMemStateStore()
	mockConsole := &mockAccessConsole{
		responses: map[string]string{
			"/whitelist on": "Whitelist is already turned on",
			"/list":         "There are 1 of a max of 20 players online: notch",
		},
	}
	mcAccess := mcaccess.NewAccessManager(stateStore, "manifests/minecraft-modded/access.yaml", mockConsole)

	h := New(Config{
		MCAccess:   mcAccess,
		StateStore: stateStore,
	})
	app := fiber.New()
	h.Register(app)

	// Page load
	reqPage := httptest.NewRequest(http.MethodGet, "/minecraft/access", nil)
	respPage, err := app.Test(reqPage)
	if err != nil || respPage.StatusCode != fiber.StatusOK {
		t.Fatalf("access page failed: %v, status: %d", err, respPage.StatusCode)
	}

	// 1. HTML Form GrantOp
	reqGrant := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/op", strings.NewReader("username=steve"))
	reqGrant.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqGrant.Header.Set("Accept", "text/html")
	respGrant, err := app.Test(reqGrant)
	if err != nil || respGrant.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("grant op html expected 303, got %d", respGrant.StatusCode)
	}

	// Grant op again (unchanged)
	reqGrantAgain := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/op", strings.NewReader("username=steve"))
	reqGrantAgain.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqGrantAgain.Header.Set("Accept", "text/html")
	respGrant2, _ := app.Test(reqGrantAgain)
	if respGrant2.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("grant op again expected 303, got %d", respGrant2.StatusCode)
	}

	// 2. HTML Form RevokeOp
	reqRevoke := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/deop", strings.NewReader("username=steve"))
	reqRevoke.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqRevoke.Header.Set("Accept", "text/html")
	respRevoke, _ := app.Test(reqRevoke)
	if respRevoke.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("revoke op expected 303, got %d", respRevoke.StatusCode)
	}

	// Revoke op again (unchanged)
	reqRevokeAgain := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/deop", strings.NewReader("username=steve"))
	reqRevokeAgain.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqRevokeAgain.Header.Set("Accept", "text/html")
	respRevoke2, _ := app.Test(reqRevokeAgain)
	if respRevoke2.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("revoke op again expected 303, got %d", respRevoke2.StatusCode)
	}

	// 3. HTML Form AddWhitelist & RemoveWhitelist
	reqWLAdd := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/whitelist/add", strings.NewReader("username=alex"))
	reqWLAdd.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqWLAdd.Header.Set("Accept", "text/html")
	respWLAdd, _ := app.Test(reqWLAdd)
	if respWLAdd.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("add whitelist expected 303, got %d", respWLAdd.StatusCode)
	}

	reqWLAddAgain := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/whitelist/add", strings.NewReader("username=alex"))
	reqWLAddAgain.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqWLAddAgain.Header.Set("Accept", "text/html")
	respWLAdd2, _ := app.Test(reqWLAddAgain)
	if respWLAdd2.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("add whitelist again expected 303, got %d", respWLAdd2.StatusCode)
	}

	reqWLRem := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/whitelist/remove", strings.NewReader("username=alex"))
	reqWLRem.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqWLRem.Header.Set("Accept", "text/html")
	respWLRem, _ := app.Test(reqWLRem)
	if respWLRem.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("rem whitelist expected 303, got %d", respWLRem.StatusCode)
	}

	reqWLRemAgain := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/whitelist/remove", strings.NewReader("username=alex"))
	reqWLRemAgain.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqWLRemAgain.Header.Set("Accept", "text/html")
	respWLRem2, _ := app.Test(reqWLRemAgain)
	if respWLRem2.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("rem whitelist again expected 303, got %d", respWLRem2.StatusCode)
	}

	// 4. Whitelist toggle (JSON + HTML)
	reqToggleJSON := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/whitelist/toggle", nil)
	reqToggleJSON.Header.Set("Accept", "application/json")
	respToggleJSON, _ := app.Test(reqToggleJSON)
	if respToggleJSON.StatusCode != fiber.StatusOK {
		t.Fatalf("toggle JSON expected 200, got %d", respToggleJSON.StatusCode)
	}

	reqToggleHTML := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/whitelist/toggle", nil)
	reqToggleHTML.Header.Set("Accept", "text/html")
	respToggleHTML, _ := app.Test(reqToggleHTML)
	if respToggleHTML.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("toggle HTML expected 303, got %d", respToggleHTML.StatusCode)
	}

	// 5. Empty username validation
	reqEmpty := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/op", strings.NewReader("username="))
	reqEmpty.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqEmpty.Header.Set("Accept", "text/html")
	respEmpty, _ := app.Test(reqEmpty)
	if respEmpty.StatusCode != fiber.StatusSeeOther {
		t.Errorf("empty user HTML expected 303, got %d", respEmpty.StatusCode)
	}

	reqEmptyJSON := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/op", strings.NewReader("username="))
	reqEmptyJSON.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqEmptyJSON.Header.Set("Accept", "application/json")
	respEmptyJSON, _ := app.Test(reqEmptyJSON)
	if respEmptyJSON.StatusCode != fiber.StatusBadRequest {
		t.Errorf("empty user JSON expected 400, got %d", respEmptyJSON.StatusCode)
	}

	// 6. Unconfigured
	hUnconf := New(Config{})
	appUnconf := fiber.New()
	hUnconf.Register(appUnconf)
	reqUnconfHTML := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/op", nil)
	reqUnconfHTML.Header.Set("Accept", "text/html")
	respUnconfHTML, _ := appUnconf.Test(reqUnconfHTML)
	if respUnconfHTML.StatusCode != fiber.StatusSeeOther {
		t.Errorf("unconf HTML expected 303, got %d", respUnconfHTML.StatusCode)
	}

	reqUnconfJSON := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/op", nil)
	reqUnconfJSON.Header.Set("Accept", "application/json")
	respUnconfJSON, _ := appUnconf.Test(reqUnconfJSON)
	if respUnconfJSON.StatusCode != fiber.StatusServiceUnavailable {
		t.Errorf("unconf JSON expected 503, got %d", respUnconfJSON.StatusCode)
	}
}

type failPatchStore struct {
	ports.StateStore
}

func (f *failPatchStore) Patch(ctx context.Context, path, msg string, mutate func(*ports.Document) (bool, error)) (bool, error) {
	return false, errors.New("git patch error")
}

type failConsole struct{}

func (f *failConsole) Execute(cmd string) (string, error) {
	return "", errors.New("rcon failure")
}

type mockPlayerReader struct {
	players []domain.Player
}

func (m mockPlayerReader) ListPlayers() ([]domain.Player, error) {
	return m.players, nil
}

func TestAccessRemainingEdges(t *testing.T) {
	app := fiber.New()

	// 1. Actor fallback
	hActor := New(Config{})
	var fastCtx fasthttp.RequestCtx
	c := app.AcquireCtx(&fastCtx)
	defer app.ReleaseCtx(c)

	if a := hActor.cfg.Actor(c); a != "-" {
		t.Errorf("expected '-', got %q", a)
	}
	c.Locals("actor", "superadmin")
	if a := hActor.cfg.Actor(c); a != "superadmin" {
		t.Errorf("expected 'superadmin', got %q", a)
	}

	// 2. AdminsPage with Players, Admins, and Valheim instances with empty LBIP
	dbPath := filepath.Join(t.TempDir(), "access_edges.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	mockPlayers := mockPlayerReader{
		players: []domain.Player{{SteamID: "76561198000000001", Character: "Valkyrie"}},
	}
	state := newMemStateStore()
	adm := admins.New(state, "valheim-admins.yaml", admins.WithAudit(st))
	_, _ = adm.Grant(context.Background(), "76561198000000001", "admin")

	vhRepo := store.NewValheimInstanceRepo(st)
	_ = vhRepo.Upsert(domain.Instance{
		Number: 1, Name: "Midgard", State: domain.StateRunning, GameID: domain.GameValheim, Password: "secret", LBIP: "",
	})
	vhMgr := instances.NewInstanceManager(vhRepo, nil, nil, 16, 4, 2, "manifests/valheim", "192.168.20.224", nil, "valheim", instances.WithGameID(domain.GameValheim))

	var capturedFilter func(string) bool
	hFull := New(Config{
		Admins:           adm,
		Players:          mockPlayers,
		ValheimInstances: vhMgr,
		ApplyAfterSync: func(cmName, key string, want func(string) bool) {
			capturedFilter = want
		},
	})
	appFull := fiber.New()
	hFull.Register(appFull)

	respAdmins, err := appFull.Test(httptest.NewRequest(http.MethodGet, "/admins", nil))
	if err != nil || respAdmins.StatusCode != http.StatusOK {
		t.Fatalf("GET /admins failed: %v, status: %d", err, respAdmins.StatusCode)
	}

	// 3. Valheim AdminsGrant and AdminsRevoke
	// 3a. Empty steam_id
	respGrantEmpty, _ := appFull.Test(httptest.NewRequest(http.MethodPost, "/admins/grant", strings.NewReader("steam_id=")))
	if respGrantEmpty.StatusCode != fiber.StatusSeeOther {
		t.Errorf("empty grant status = %d, want 303", respGrantEmpty.StatusCode)
	}
	respRevokeEmpty, _ := appFull.Test(httptest.NewRequest(http.MethodPost, "/admins/revoke", strings.NewReader("steam_id=")))
	if respRevokeEmpty.StatusCode != fiber.StatusSeeOther {
		t.Errorf("empty revoke status = %d, want 303", respRevokeEmpty.StatusCode)
	}

	// 3b. Grant when already an admin (unchanged branch)
	reqGrantAgain := httptest.NewRequest(http.MethodPost, "/admins/grant", strings.NewReader("steam_id=76561198000000001"))
	reqGrantAgain.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respGrantAgain, _ := appFull.Test(reqGrantAgain)
	if respGrantAgain.StatusCode != fiber.StatusSeeOther {
		t.Errorf("grant again status = %d, want 303", respGrantAgain.StatusCode)
	}

	// 3c. Grant new admin with ApplyAfterSync filter exercise
	reqGrantNew := httptest.NewRequest(http.MethodPost, "/admins/grant", strings.NewReader("steam_id=76561198000000002"))
	reqGrantNew.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respGrantNew, _ := appFull.Test(reqGrantNew)
	if respGrantNew.StatusCode != fiber.StatusSeeOther {
		t.Errorf("grant new status = %d, want 303", respGrantNew.StatusCode)
	}
	if capturedFilter != nil {
		if !capturedFilter("76561198000000002") {
			t.Errorf("expected filter true for matching ID")
		}
		if capturedFilter("other_id") {
			t.Errorf("expected filter false for non-matching ID")
		}
	}

	// 3d. Revoke admin with ApplyAfterSync filter exercise
	capturedFilter = nil
	reqRevokeNew := httptest.NewRequest(http.MethodPost, "/admins/revoke", strings.NewReader("steam_id=76561198000000002"))
	reqRevokeNew.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respRevokeNew, _ := appFull.Test(reqRevokeNew)
	if respRevokeNew.StatusCode != fiber.StatusSeeOther {
		t.Errorf("revoke new status = %d, want 303", respRevokeNew.StatusCode)
	}
	if capturedFilter != nil {
		if !capturedFilter("other_id") {
			t.Errorf("expected filter true for non-matching ID on revoke")
		}
		if capturedFilter("76561198000000002") {
			t.Errorf("expected filter false for matching ID on revoke")
		}
	}

	// 3e. Revoke admin when not an admin (unchanged branch)
	reqRevokeAgain := httptest.NewRequest(http.MethodPost, "/admins/revoke", strings.NewReader("steam_id=76561198000000002"))
	reqRevokeAgain.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respRevokeAgain, _ := appFull.Test(reqRevokeAgain)
	if respRevokeAgain.StatusCode != fiber.StatusSeeOther {
		t.Errorf("revoke again status = %d, want 303", respRevokeAgain.StatusCode)
	}

	// 3f. AdminsGrant and AdminsRevoke with store error
	failAdm := admins.New(&failPatchStore{}, "valheim-admins.yaml")
	hFailAdm := New(Config{Admins: failAdm})
	appFailAdm := fiber.New()
	hFailAdm.Register(appFailAdm)

	reqFailGrant := httptest.NewRequest(http.MethodPost, "/admins/grant", strings.NewReader("steam_id=123"))
	reqFailGrant.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respFailGrant, _ := appFailAdm.Test(reqFailGrant)
	if respFailGrant.StatusCode != fiber.StatusSeeOther {
		t.Errorf("fail grant status = %d, want 303", respFailGrant.StatusCode)
	}

	reqFailRevoke := httptest.NewRequest(http.MethodPost, "/admins/revoke", strings.NewReader("steam_id=123"))
	reqFailRevoke.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respFailRevoke, _ := appFailAdm.Test(reqFailRevoke)
	if respFailRevoke.StatusCode != fiber.StatusSeeOther {
		t.Errorf("fail revoke status = %d, want 303", respFailRevoke.StatusCode)
	}

	// 4. Minecraft Access: empty usernames for deop, whitelist add, whitelist remove
	hMC := New(Config{
		MCAccess: mcaccess.NewAccessManager(newMemStateStore(), "access.yaml", nil),
	})
	appMC := fiber.New()
	hMC.Register(appMC)

	endpoints := []string{
		"/api/minecraft/access/deop",
		"/api/minecraft/access/whitelist/add",
		"/api/minecraft/access/whitelist/remove",
	}
	for _, ep := range endpoints {
		reqHTML := httptest.NewRequest(http.MethodPost, ep, strings.NewReader("username="))
		reqHTML.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		reqHTML.Header.Set("Accept", "text/html")
		respHTML, _ := appMC.Test(reqHTML)
		if respHTML.StatusCode != fiber.StatusSeeOther {
			t.Errorf("%s empty user HTML status = %d, want 303", ep, respHTML.StatusCode)
		}

		reqJSON := httptest.NewRequest(http.MethodPost, ep, strings.NewReader("username="))
		reqJSON.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		reqJSON.Header.Set("Accept", "application/json")
		respJSON, _ := appMC.Test(reqJSON)
		if respJSON.StatusCode != fiber.StatusBadRequest {
			t.Errorf("%s empty user JSON status = %d, want 400", ep, respJSON.StatusCode)
		}
	}

	// 5. Minecraft Access: Patch/Git errors (HTML and JSON)
	hMCFail := New(Config{
		MCAccess: mcaccess.NewAccessManager(&failPatchStore{}, "access.yaml", nil),
	})
	appMCFail := fiber.New()
	hMCFail.Register(appMCFail)

	allOps := []string{
		"/api/minecraft/access/op",
		"/api/minecraft/access/deop",
		"/api/minecraft/access/whitelist/add",
		"/api/minecraft/access/whitelist/remove",
	}
	for _, ep := range allOps {
		reqHTML := httptest.NewRequest(http.MethodPost, ep, strings.NewReader("username=alex"))
		reqHTML.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		reqHTML.Header.Set("Accept", "text/html")
		respHTML, _ := appMCFail.Test(reqHTML)
		if respHTML.StatusCode != fiber.StatusSeeOther {
			t.Errorf("%s fail HTML status = %d, want 303", ep, respHTML.StatusCode)
		}

		reqJSON := httptest.NewRequest(http.MethodPost, ep, strings.NewReader("username=alex"))
		reqJSON.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		reqJSON.Header.Set("Accept", "application/json")
		respJSON, _ := appMCFail.Test(reqJSON)
		if respJSON.StatusCode != fiber.StatusInternalServerError {
			t.Errorf("%s fail JSON status = %d, want 500", ep, respJSON.StatusCode)
		}
	}

	// 6. Whitelist Toggle: console error
	hToggleFail := New(Config{
		MCAccess: mcaccess.NewAccessManager(newMemStateStore(), "access.yaml", &failConsole{}),
	})
	appToggleFail := fiber.New()
	hToggleFail.Register(appToggleFail)

	reqToggleFailHTML := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/whitelist/toggle", nil)
	reqToggleFailHTML.Header.Set("Accept", "text/html")
	respToggleFailHTML, _ := appToggleFail.Test(reqToggleFailHTML)
	if respToggleFailHTML.StatusCode != fiber.StatusSeeOther {
		t.Errorf("toggle fail HTML status = %d, want 303", respToggleFailHTML.StatusCode)
	}

	reqToggleFailJSON := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/whitelist/toggle", nil)
	reqToggleFailJSON.Header.Set("Accept", "application/json")
	respToggleFailJSON, _ := appToggleFail.Test(reqToggleFailJSON)
	if respToggleFailJSON.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("toggle fail JSON status = %d, want 500", respToggleFailJSON.StatusCode)
	}

	// 7. Whitelist Toggle: turned off branch (!target)
	mockConsoleOff := &mockAccessConsole{
		responses: map[string]string{
			"/whitelist on":  "Whitelist is already turned on",
			"/whitelist off": "Whitelist is now turned off",
		},
	}
	hToggleOff := New(Config{
		MCAccess: mcaccess.NewAccessManager(newMemStateStore(), "access.yaml", mockConsoleOff),
	})
	appToggleOff := fiber.New()
	hToggleOff.Register(appToggleOff)

	reqToggleOffHTML := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/whitelist/toggle", nil)
	reqToggleOffHTML.Header.Set("Accept", "text/html")
	respToggleOffHTML, _ := appToggleOff.Test(reqToggleOffHTML)
	if respToggleOffHTML.StatusCode != fiber.StatusSeeOther {
		t.Errorf("toggle off HTML status = %d, want 303", respToggleOffHTML.StatusCode)
	}

	reqToggleOffJSON := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/whitelist/toggle", nil)
	reqToggleOffJSON.Header.Set("Accept", "application/json")
	respToggleOffJSON, _ := appToggleOff.Test(reqToggleOffJSON)
	if respToggleOffJSON.StatusCode != fiber.StatusOK {
		t.Errorf("toggle off JSON status = %d, want 200", respToggleOffJSON.StatusCode)
	}
	bodyToggleOffJSON, _ := io.ReadAll(respToggleOffJSON.Body)
	if !strings.Contains(string(bodyToggleOffJSON), `"enforced":false`) {
		t.Errorf("expected enforced:false, got %s", string(bodyToggleOffJSON))
	}

	// 8. Whitelist Toggle: SetWhitelistEnforced error
	mockConsoleSetErr := &mockAccessConsole{
		responses: map[string]string{
			"/whitelist on": "Whitelist is already turned on",
		},
		errs: map[string]error{
			"/whitelist off": errors.New("cannot set whitelist"),
		},
	}
	hToggleSetErr := New(Config{
		MCAccess: mcaccess.NewAccessManager(newMemStateStore(), "access.yaml", mockConsoleSetErr),
	})
	appToggleSetErr := fiber.New()
	hToggleSetErr.Register(appToggleSetErr)

	reqSetErrHTML := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/whitelist/toggle", nil)
	reqSetErrHTML.Header.Set("Accept", "text/html")
	respSetErrHTML, _ := appToggleSetErr.Test(reqSetErrHTML)
	if respSetErrHTML.StatusCode != fiber.StatusSeeOther {
		t.Errorf("set err HTML status = %d, want 303", respSetErrHTML.StatusCode)
	}

	reqSetErrJSON := httptest.NewRequest(http.MethodPost, "/api/minecraft/access/whitelist/toggle", nil)
	reqSetErrJSON.Header.Set("Accept", "application/json")
	respSetErrJSON, _ := appToggleSetErr.Test(reqSetErrJSON)
	if respSetErrJSON.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("set err JSON status = %d, want 500", respSetErrJSON.StatusCode)
	}
}


