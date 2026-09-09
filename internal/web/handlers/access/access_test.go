package access

import (
	mcaccess "agrelha/internal/app/access"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"agrelha/internal/app/admins"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"

	"github.com/gofiber/fiber/v2"
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
