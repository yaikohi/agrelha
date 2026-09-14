package content

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type fakeCatalog struct {
	items   map[string]domain.ModSearchResult
	version string
	deps    []string
	readme  string
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
	return f.version, f.deps, nil
}
func (f *fakeCatalog) Readme(ctx context.Context, ns, name, version string) (string, error) {
	return f.readme, nil
}
func (f *fakeCatalog) ResolveTree(ctx context.Context, ns, name string) ([]string, error) {
	return nil, nil
}

type fakeAudit struct{}

func (f *fakeAudit) RecordAudit(actor, action, detail string) error { return nil }

func TestPublicHost(t *testing.T) {
	origLookup := lookupIP
	defer func() { lookupIP = origLookup }()

	if PublicHost("127.0.0.1") {
		t.Errorf("expected 127.0.0.1 to not be public")
	}
	if PublicHost("localhost") {
		t.Errorf("expected localhost to not be public")
	}
	if PublicHost("") {
		t.Errorf("expected empty host to not be public")
	}

	// Lookup error
	lookupIP = func(host string) ([]net.IP, error) {
		return nil, errors.New("dns lookup error")
	}
	if PublicHost("error.host") {
		t.Errorf("expected false on lookup error")
	}

	// Empty IPs
	lookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{}, nil
	}
	if PublicHost("empty.host") {
		t.Errorf("expected false on empty IPs")
	}

	// Unspecified IP (0.0.0.0)
	lookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("0.0.0.0")}, nil
	}
	if PublicHost("unspecified.host") {
		t.Errorf("expected false for unspecified IP")
	}

	// Link-local unicast (169.254.1.1)
	lookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("169.254.1.1")}, nil
	}
	if PublicHost("linklocal.host") {
		t.Errorf("expected false for link local unicast")
	}

	// Link-local multicast (224.0.0.1)
	lookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("224.0.0.1")}, nil
	}
	if PublicHost("multicast.host") {
		t.Errorf("expected false for link local multicast")
	}

	// Public IP
	lookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	if !PublicHost("example.com") {
		t.Errorf("expected true for public IP")
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

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type failStateStore struct {
	ports.StateStore
}

func (f *failStateStore) Get(ctx context.Context, path string) (ports.Document, error) {
	return ports.Document{}, errors.New("get error")
}

func (f *failStateStore) Patch(ctx context.Context, path string, msg string, mutate func(doc *ports.Document) (bool, error)) (bool, error) {
	return false, errors.New("patch error")
}

func TestContentEdges(t *testing.T) {
	app := fiber.New()
	hBase := New(Config{})
	hBase.Register(app)

	// 1. Actor fallback
	hActor := New(Config{})
	var fastCtx fasthttp.RequestCtx
	c := app.AcquireCtx(&fastCtx)
	defer app.ReleaseCtx(c)

	if a := hActor.cfg.Actor(c); a != "local" {
		t.Errorf("expected 'local', got %q", a)
	}
	c.Locals("actor", "supermod")
	if a := hActor.cfg.Actor(c); a != "supermod" {
		t.Errorf("expected 'supermod', got %q", a)
	}

	// 2. configData branches & ConfigsList error
	hErrData := New(Config{
		ConfigData: func(ctx context.Context) (map[string]string, error) {
			return nil, errors.New("data error")
		},
	})
	appErrData := fiber.New()
	var fastCtxErr fasthttp.RequestCtx
	cErr := appErrData.AcquireCtx(&fastCtxErr)
	defer appErrData.ReleaseCtx(cErr)

	if _, err := hErrData.ConfigsList(cErr); err == nil {
		t.Errorf("expected error from ConfigsList when configData fails")
	}

	// configData fallback to StateStore.Get (with error and with success)
	hFailStore := New(Config{
		StateStore:     &failStateStore{},
		ModConfigsPath: "configs.yaml",
	})
	if _, err := hFailStore.configData(context.Background()); err == nil {
		t.Errorf("expected error from configData on StateStore.Get failure")
	}

	memStore := &memStateStore{
		docs: map[string]*ports.Document{
			"configs.yaml": {Data: map[string]string{"foo.cfg": "bar"}},
		},
	}
	hMemStore := New(Config{
		StateStore:     memStore,
		ModConfigsPath: "configs.yaml",
	})
	data, err := hMemStore.configData(context.Background())
	if err != nil || data["foo.cfg"] != "bar" {
		t.Errorf("expected foo.cfg=bar, got %v, err=%v", data, err)
	}

	// configData with nil StateStore
	hNilStore := New(Config{})
	nilData, err := hNilStore.configData(context.Background())
	if err != nil || nilData != nil {
		t.Errorf("expected nil data on nil store")
	}

	// 3. ConfigSave & ConfigDelete with Patch error, doc.Data == nil, and ApplyAfterSync
	var appliedCheck func(string) bool
	hConfigs := New(Config{
		StateStore:     memStore,
		ModConfigsPath: "configs.yaml",
		ApplyAfterSync: func(cmName, key string, check func(string) bool) {
			appliedCheck = check
		},
	})
	appConfigs := fiber.New()
	hConfigs.Register(appConfigs)

	// Save with doc.Data == nil
	emptyDocStore := &memStateStore{
		docs: map[string]*ports.Document{
			"configs.yaml": {Data: nil},
		},
	}
	hEmptyDoc := New(Config{
		StateStore:     emptyDocStore,
		ModConfigsPath: "configs.yaml",
		Audit:          &fakeAudit{},
	})
	appEmptyDoc := fiber.New()
	hEmptyDoc.Register(appEmptyDoc)

	reqSaveInit := httptest.NewRequest("POST", "/configs/save", strings.NewReader("file=New.Mod.cfg&content=hello"))
	reqSaveInit.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respSaveInit, _ := appEmptyDoc.Test(reqSaveInit)
	if respSaveInit.StatusCode != fiber.StatusSeeOther {
		t.Errorf("save init expected 303, got %d", respSaveInit.StatusCode)
	}

	// Save with ApplyAfterSync filter exercise
	reqSave := httptest.NewRequest("POST", "/configs/save", strings.NewReader("file=Apply.Mod.cfg&content=target-content"))
	reqSave.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respSave, _ := appConfigs.Test(reqSave)
	if respSave.StatusCode != fiber.StatusSeeOther {
		t.Errorf("save expected 303, got %d", respSave.StatusCode)
	}
	if appliedCheck != nil {
		if !appliedCheck("target-content") {
			t.Errorf("expected check true for target-content")
		}
		if appliedCheck("other") {
			t.Errorf("expected check false for other content")
		}
	}

	// Save with Patch error
	hFailPatch := New(Config{
		StateStore:     &failStateStore{},
		ModConfigsPath: "configs.yaml",
	})
	appFailPatch := fiber.New()
	hFailPatch.Register(appFailPatch)

	reqSaveErr := httptest.NewRequest("POST", "/configs/save", strings.NewReader("file=Fail.Mod.cfg&content=fail"))
	reqSaveErr.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respSaveErr, _ := appFailPatch.Test(reqSaveErr)
	if respSaveErr.StatusCode != fiber.StatusSeeOther {
		t.Errorf("save err expected 303, got %d", respSaveErr.StatusCode)
	}

	// Delete with doc.Data == nil
	emptyDocStore2 := &memStateStore{
		docs: map[string]*ports.Document{
			"configs.yaml": {Data: nil},
		},
	}
	hEmptyDoc2 := New(Config{
		StateStore:     emptyDocStore2,
		ModConfigsPath: "configs.yaml",
	})
	appEmptyDoc2 := fiber.New()
	hEmptyDoc2.Register(appEmptyDoc2)

	reqDelNil := httptest.NewRequest("POST", "/configs/delete", strings.NewReader("file=NotThere.Mod.cfg"))
	reqDelNil.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respDelNil, _ := appEmptyDoc2.Test(reqDelNil)
	if respDelNil.StatusCode != fiber.StatusSeeOther {
		t.Errorf("delete nil data expected 303, got %d", respDelNil.StatusCode)
	}

	// Delete existing with ApplyAfterSync filter exercise
	appliedCheck = nil
	reqDelExisting := httptest.NewRequest("POST", "/configs/delete", strings.NewReader("file=Apply.Mod.cfg"))
	reqDelExisting.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respDelExisting, _ := appConfigs.Test(reqDelExisting)
	if respDelExisting.StatusCode != fiber.StatusSeeOther {
		t.Errorf("delete existing expected 303, got %d", respDelExisting.StatusCode)
	}
	if appliedCheck != nil {
		if !appliedCheck("") {
			t.Errorf("expected delete check true for empty string")
		}
		if appliedCheck("something") {
			t.Errorf("expected delete check false for non-empty string")
		}
	}

	// Delete with Patch error
	reqDelErr := httptest.NewRequest("POST", "/configs/delete", strings.NewReader("file=Fail.Mod.cfg"))
	reqDelErr.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respDelErr, _ := appFailPatch.Test(reqDelErr)
	if respDelErr.StatusCode != fiber.StatusSeeOther {
		t.Errorf("delete err expected 303, got %d", respDelErr.StatusCode)
	}

	// 4. ModDetail: owner & name fallback, version fallback, and prettyDeps
	cat := &fakeCatalog{
		items: map[string]domain.ModSearchResult{
			"some/unnamed": {
				Owner:   "",
				Name:    "",
				Version: "",
			},
		},
		version: "2.1.0",
		deps: []string{
			"short",
			"denikson-BepInExPack_Valheim-5.4.2202",
			"author-fancy_tool-1.2.3",
		},
		readme: "# Mod Readme\nThis is a test readme.",
	}
	rc := &fakeReadmeCache{cache: make(map[string]string)}
	hMod := New(Config{
		TS:          cat,
		ReadmeCache: rc,
	})
	appMod := fiber.New()
	hMod.Register(appMod)

	reqMod := httptest.NewRequest("GET", "/mods/some/unnamed", nil)
	respMod, err := appMod.Test(reqMod)
	if err != nil || respMod.StatusCode != fiber.StatusOK {
		t.Fatalf("mod detail expected 200, got status %d, err %v", respMod.StatusCode, err)
	}
	bodyMod, _ := io.ReadAll(respMod.Body)
	if !strings.Contains(string(bodyMod), "author/fancy_tool (1.2.3)") {
		t.Errorf("expected prettyDeps in mod detail body: %s", string(bodyMod))
	}
	if !strings.Contains(string(bodyMod), "Mod Readme") {
		t.Errorf("expected rendered readme in mod detail body: %s", string(bodyMod))
	}

	// 5. imgHTTP.CheckRedirect
	via5 := make([]*http.Request, 5)
	errVia5 := imgHTTP.CheckRedirect(nil, via5)
	if errVia5 == nil || !strings.Contains(errVia5.Error(), "too many redirects") {
		t.Errorf("expected too many redirects, got %v", errVia5)
	}

	reqBadScheme, _ := http.NewRequest("GET", "http://example.com/img.png", nil)
	errBadScheme := imgHTTP.CheckRedirect(reqBadScheme, nil)
	if errBadScheme == nil || !strings.Contains(errBadScheme.Error(), "blocked redirect target") {
		t.Errorf("expected blocked redirect target for http scheme, got %v", errBadScheme)
	}

	reqBadHost, _ := http.NewRequest("GET", "https://127.0.0.1/img.png", nil)
	errBadHost := imgHTTP.CheckRedirect(reqBadHost, nil)
	if errBadHost == nil || !strings.Contains(errBadHost.Error(), "blocked redirect target") {
		t.Errorf("expected blocked redirect target for private host, got %v", errBadHost)
	}

	origLookup := lookupIP
	defer func() { lookupIP = origLookup }()
	lookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}

	reqGoodRedirect, _ := http.NewRequest("GET", "https://example.com/img.png", nil)
	if err := imgHTTP.CheckRedirect(reqGoodRedirect, nil); err != nil {
		t.Errorf("expected nil error on good redirect, got %v", err)
	}

	// 6. ImageProxy through imgHTTP.Transport
	origTransport := imgHTTP.Transport
	defer func() { imgHTTP.Transport = origTransport }()

	// 6a. ImageProxy: success
	imgHTTP.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header: http.Header{
				"Content-Type": []string{"image/png"},
			},
			Body: io.NopCloser(bytes.NewReader([]byte("\x89PNG\r\n\x1a\nfakeimage"))),
		}, nil
	})
	reqImgSuccess := httptest.NewRequest("GET", "/img?u=https://example.com/pic.png", nil)
	respImgSuccess, err := app.Test(reqImgSuccess)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if respImgSuccess.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200 for image proxy success, got %d", respImgSuccess.StatusCode)
	}

	// 6b. ImageProxy: upstream error status (500)
	imgHTTP.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Status:     "500 Internal Server Error",
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("server error")),
		}, nil
	})
	reqImg500 := httptest.NewRequest("GET", "/img?u=https://example.com/pic.png", nil)
	respImg500, _ := app.Test(reqImg500)
	if respImg500.StatusCode != fiber.StatusBadGateway {
		t.Errorf("expected 502 for upstream error, got %d", respImg500.StatusCode)
	}

	// 6c. ImageProxy: not an image
	imgHTTP.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header: http.Header{
				"Content-Type": []string{"text/html"},
			},
			Body: io.NopCloser(strings.NewReader("<html></html>")),
		}, nil
	})
	reqImgHTML := httptest.NewRequest("GET", "/img?u=https://example.com/pic.png", nil)
	respImgHTML, _ := app.Test(reqImgHTML)
	if respImgHTML.StatusCode != fiber.StatusUnsupportedMediaType {
		t.Errorf("expected 415 for non-image, got %d", respImgHTML.StatusCode)
	}

	// 6d. ImageProxy: fetch failed (transport error)
	imgHTTP.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("network failure")
	})
	reqImgFail := httptest.NewRequest("GET", "/img?u=https://example.com/pic.png", nil)
	respImgFail, _ := app.Test(reqImgFail)
	if respImgFail.StatusCode != fiber.StatusBadGateway {
		t.Errorf("expected 502 for network failure, got %d", respImgFail.StatusCode)
	}
}


