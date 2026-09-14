package content

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type fakeCatalog struct {
	items map[string]domain.ModSearchResult
}

func (f *fakeCatalog) Get(fullName string) (domain.ModSearchResult, bool) {
	m, ok := f.items[fullName]
	return m, ok
}
func (f *fakeCatalog) Search(ctx context.Context, query string, limit int) ([]domain.ModSearchResult, error) {
	return nil, nil
}
func (f *fakeCatalog) Ready() bool { return true }
func (f *fakeCatalog) LatestVersion(ctx context.Context, ns, name string) (string, []string, error) {
	return "", nil, nil
}
func (f *fakeCatalog) Readme(ctx context.Context, ns, name, version string) (string, error) {
	return "", nil
}
func (f *fakeCatalog) ResolveTree(ctx context.Context, ns, name string) ([]string, error) {
	return nil, nil
}

type fakeAudit struct{}

func (f *fakeAudit) RecordAudit(actor, action, detail string) error { return nil }

func TestPublicHost(t *testing.T) {
	if PublicHost("127.0.0.1") {
		t.Errorf("expected 127.0.0.1 to not be public")
	}
	if PublicHost("localhost") {
		t.Errorf("expected localhost to not be public")
	}
	if PublicHost("") {
		t.Errorf("expected empty host to not be public")
	}
}

func TestConfigValidation(t *testing.T) {
	valid := []string{"foo.cfg", "com.author.mod.cfg", "My_Mod-1.cfg"}
	for _, f := range valid {
		if !cfgNameRe.MatchString(f) {
			t.Errorf("expected %q to be valid config file name", f)
		}
	}
	invalid := []string{"foo.txt", "../foo.cfg", "foo/bar.cfg", ".hidden.cfg"}
	for _, f := range invalid {
		if cfgNameRe.MatchString(f) {
			t.Errorf("expected %q to be invalid config file name", f)
		}
	}
}

type fakeValheimGame struct {
	bundle domain.Bundle
}

func (f *fakeValheimGame) ID() domain.GameID                  { return domain.GameValheim }
func (f *fakeValheimGame) Display() domain.Display            { return domain.Display{Name: "Valheim"} }
func (f *fakeValheimGame) Providers() []ports.ContentProvider { return nil }
func (f *fakeValheimGame) ResolveContent(ctx context.Context, inst domain.Instance) (domain.ContentSet, error) {
	return domain.ContentSet{}, nil
}
func (f *fakeValheimGame) ExportClientBundle(ctx context.Context, inst domain.Instance) (domain.Bundle, error) {
	return f.bundle, nil
}
func (f *fakeValheimGame) RuntimeSpec(inst domain.Instance) domain.RuntimeSpec {
	return domain.RuntimeSpec{}
}
func (f *fakeValheimGame) Telemetry(ctx context.Context) (domain.GameTelemetry, error) {
	return domain.GameTelemetry{}, nil
}
func (f *fakeValheimGame) AdmissionModel() domain.AdmissionModel { return domain.AdmissionPassword }
func (f *fakeValheimGame) OperatorIDKind() domain.OperatorIDKind { return domain.IDKindSteam64 }

type memStateStore struct {
	mu   sync.Mutex
	docs map[string]*ports.Document
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

func (m *memStateStore) Put(ctx context.Context, path string, doc ports.Document, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs[path] = &doc
	return nil
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

type fakeReadmeCache struct {
	cache map[string]string
}

func (f *fakeReadmeCache) GetReadme(key, version string) (string, bool, error) {
	v, ok := f.cache[key+"@"+version]
	return v, ok, nil
}

func (f *fakeReadmeCache) PutReadme(key, version, md string) error {
	f.cache[key+"@"+version] = md
	return nil
}

func TestContentConfigs(t *testing.T) {
	store := &memStateStore{
		docs: map[string]*ports.Document{
			"configs.yaml": {
				Data: map[string]string{
					"Valheim.Plus.cfg": "enabled=true\n",
				},
			},
		},
	}

	h := New(Config{
		ModConfigsPath: "configs.yaml",
		StateStore:     store,
		Audit:          &fakeAudit{},
	})

	app := fiber.New()
	h.Register(app)

	// 1. Configs Page
	reqPage := httptest.NewRequest("GET", "/configs", nil)
	respPage, err := app.Test(reqPage)
	if err != nil || respPage.StatusCode != fiber.StatusOK {
		t.Fatalf("configs page failed: %v, status: %d", err, respPage.StatusCode)
	}

	// 2. Config New
	reqNew := httptest.NewRequest("GET", "/configs/new", nil)
	respNew, err := app.Test(reqNew)
	if err != nil || respNew.StatusCode != fiber.StatusOK {
		t.Fatalf("config new failed: %v, status: %d", err, respNew.StatusCode)
	}

	// 3. Config Edit (valid)
	reqEdit := httptest.NewRequest("GET", "/configs/edit?f=Valheim.Plus.cfg", nil)
	respEdit, err := app.Test(reqEdit)
	if err != nil || respEdit.StatusCode != fiber.StatusOK {
		t.Fatalf("config edit failed: %v, status: %d", err, respEdit.StatusCode)
	}

	// 4. Config Edit (invalid file name -> redirects)
	reqBadEdit := httptest.NewRequest("GET", "/configs/edit?f=invalid.txt", nil)
	respBadEdit, err := app.Test(reqBadEdit)
	if err != nil || respBadEdit.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("bad config edit expected 303, got %d", respBadEdit.StatusCode)
	}

	// 5. Config Save (invalid file name)
	reqBadSave := httptest.NewRequest("POST", "/configs/save", strings.NewReader("file=bad.txt&content=abc"))
	reqBadSave.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respBadSave, err := app.Test(reqBadSave)
	if err != nil || respBadSave.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("bad save expected 303, got %d", respBadSave.StatusCode)
	}

	// 6. Config Save (valid new/modified file)
	reqSave := httptest.NewRequest("POST", "/configs/save", strings.NewReader("file=MyMod.cfg&content=hello=world"))
	reqSave.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respSave, err := app.Test(reqSave)
	if err != nil || respSave.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("save expected 303, got %d", respSave.StatusCode)
	}

	// 7. Config Save (unchanged content)
	reqUnchanged := httptest.NewRequest("POST", "/configs/save", strings.NewReader("file=MyMod.cfg&content=hello=world"))
	reqUnchanged.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respUnchanged, err := app.Test(reqUnchanged)
	if err != nil || respUnchanged.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("unchanged save expected 303, got %d", respUnchanged.StatusCode)
	}

	// 8. Config Delete (invalid file name)
	reqBadDel := httptest.NewRequest("POST", "/configs/delete", strings.NewReader("file=bad.txt"))
	reqBadDel.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respBadDel, err := app.Test(reqBadDel)
	if err != nil || respBadDel.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("bad delete expected 303, got %d", respBadDel.StatusCode)
	}

	// 9. Config Delete (valid file)
	reqDel := httptest.NewRequest("POST", "/configs/delete", strings.NewReader("file=MyMod.cfg"))
	reqDel.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respDel, err := app.Test(reqDel)
	if err != nil || respDel.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("delete expected 303, got %d", respDel.StatusCode)
	}

	// 10. Config Delete (already deleted / not present)
	reqDelAgain := httptest.NewRequest("POST", "/configs/delete", strings.NewReader("file=MyMod.cfg"))
	reqDelAgain.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respDelAgain, err := app.Test(reqDelAgain)
	if err != nil || respDelAgain.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("delete again expected 303, got %d", respDelAgain.StatusCode)
	}

	// 11. StateStore unconfigured
	hNoStore := New(Config{})
	appNoStore := fiber.New()
	hNoStore.Register(appNoStore)
	reqSaveNoStore := httptest.NewRequest("POST", "/configs/save", strings.NewReader("file=MyMod.cfg&content=foo"))
	reqSaveNoStore.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respNoStore, _ := appNoStore.Test(reqSaveNoStore)
	if respNoStore.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("save with no store expected 303, got %d", respNoStore.StatusCode)
	}

	reqDelNoStore := httptest.NewRequest("POST", "/configs/delete", strings.NewReader("file=MyMod.cfg"))
	reqDelNoStore.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respDelNoStore, _ := appNoStore.Test(reqDelNoStore)
	if respDelNoStore.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("delete with no store expected 303, got %d", respDelNoStore.StatusCode)
	}
}

func TestContentModDetail(t *testing.T) {
	cat := &fakeCatalog{
		items: map[string]domain.ModSearchResult{
			"author/mod": {
				Owner:       "author",
				Name:        "mod",
				Version:     "1.0.0",
				Description: "A great mod",
			},
		},
	}
	rc := &fakeReadmeCache{cache: make(map[string]string)}

	h := New(Config{
		TS:          cat,
		ReadmeCache: rc,
	})

	app := fiber.New()
	h.Register(app)

	req := httptest.NewRequest("GET", "/mods/author/mod", nil)
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("mod detail failed: %v, status: %d", err, resp.StatusCode)
	}
}

func TestImageProxy(t *testing.T) {
	h := New(Config{})
	app := fiber.New()
	h.Register(app)

	// Missing u
	reqNoU := httptest.NewRequest("GET", "/img", nil)
	respNoU, _ := app.Test(reqNoU)
	if respNoU.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for missing u, got %d", respNoU.StatusCode)
	}

	// Non-https
	reqHTTP := httptest.NewRequest("GET", "/img?u=http://example.com/test.png", nil)
	respHTTP, _ := app.Test(reqHTTP)
	if respHTTP.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for non-https, got %d", respHTTP.StatusCode)
	}

	// Private host
	reqPrivate := httptest.NewRequest("GET", "/img?u=https://127.0.0.1/test.png", nil)
	respPrivate, _ := app.Test(reqPrivate)
	if respPrivate.StatusCode != fiber.StatusForbidden {
		t.Errorf("expected 403 for private host, got %d", respPrivate.StatusCode)
	}
}

