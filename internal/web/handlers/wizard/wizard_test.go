package wizard

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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	mccontent "agrelha/internal/app/content"
	"agrelha/internal/app/instances"
	"agrelha/internal/app/modpack"
	"agrelha/internal/domain"
	"agrelha/internal/infra/manifests"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
)

type mockStore struct {
	docs map[string]ports.Document
}

func (s *mockStore) Get(_ context.Context, path string) (ports.Document, error) {
	if s.docs == nil {
		return ports.Document{}, nil
	}
	return s.docs[path], nil
}
func (s *mockStore) Put(_ context.Context, path string, doc ports.Document, _ string) error {
	if s.docs == nil {
		s.docs = make(map[string]ports.Document)
	}
	s.docs[path] = doc
	return nil
}
func (s *mockStore) Delete(_ context.Context, path, _ string) error {
	delete(s.docs, path)
	return nil
}
func (s *mockStore) PutTree(_ context.Context, _ string, tree map[string]ports.Document, _ string) error {
	if s.docs == nil {
		s.docs = make(map[string]ports.Document)
	}
	for k, v := range tree {
		s.docs[k] = v
	}
	return nil
}
func (s *mockStore) Patch(ctx context.Context, path, msg string, fn func(*ports.Document) (bool, error)) (bool, error) {
	if s.docs == nil {
		s.docs = make(map[string]ports.Document)
	}
	doc := s.docs[path]
	changed, err := fn(&doc)
	if err != nil {
		return false, err
	}
	if changed {
		s.docs[path] = doc
	}
	return changed, nil
}

func setupWizardAppWithStore(t *testing.T, cfg Config) (*fiber.App, *instances.InstanceManager, *store.Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "wizard_test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	ss := &mockStore{}
	renderer := manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded")
	instMgr := instances.NewInstanceManager(
		store.NewInstanceRepo(st), ss, nil, 32, 4, 2,
		"manifests/minecraft-modded", "192.168.20.224", renderer, "minecraft-modded",
		instances.WithGameID(domain.GameMinecraft),
	)

	if cfg.MCInstances == nil {
		cfg.MCInstances = instMgr
	}

	h := New(cfg)
	app := fiber.New()
	h.Register(app)
	return app, instMgr, st
}

func setupWizardApp(t *testing.T, cfg Config) (*fiber.App, *instances.InstanceManager) {
	app, mgr, _ := setupWizardAppWithStore(t, cfg)
	return app, mgr
}

func TestMCWizardPage(t *testing.T) {
	// 1. When MCInstances is nil -> 503 Service Unavailable
	appNil := fiber.New()
	hNil := New(Config{})
	hNil.Register(appNil)
	reqNil := httptest.NewRequest("GET", "/minecraft/create", nil)
	respNil, err := appNil.Test(reqNil)
	if err != nil || respNil.StatusCode != fiber.StatusServiceUnavailable {
		t.Errorf("expected 503 for nil MCInstances, got: %v, err: %v", respNil.StatusCode, err)
	}

	// 2. Normal rendering
	app, _ := setupWizardApp(t, Config{
		VersionReleases: func(ctx context.Context, limit int) []string {
			return []string{"1.21.1", "1.20.1"}
		},
	})
	req := httptest.NewRequest("GET", "/minecraft/create", nil)
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 OK, got: %v, err: %v", resp.StatusCode, err)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Minecraft") {
		t.Errorf("expected Minecraft page content, got: %s", string(body))
	}
}

func TestMCWizardModpacksSearch(t *testing.T) {
	// 1. SearchModpacks unconfigured
	appNoCfg, _ := setupWizardApp(t, Config{})
	req1 := httptest.NewRequest("GET", "/api/minecraft/wizard/modpacks/search?q=test", nil)
	resp1, _ := appNoCfg.Test(req1)
	body1, _ := io.ReadAll(resp1.Body)
	if !strings.Contains(string(body1), "Modpack Index unconfigured") {
		t.Errorf("expected unconfigured error, got: %s", string(body1))
	}

	// 2. Empty query prompt
	app, _ := setupWizardApp(t, Config{
		SearchModpacks: func(ctx context.Context, q string) ([]ModpackHit, error) {
			if q == "error" {
				return nil, fmt.Errorf("backend timeout")
			}
			if q == "empty" {
				return []ModpackHit{}, nil
			}
			return []ModpackHit{
				{
					ID:            101,
					Name:          "All The Mods 9",
					Summary:       "Mega pack with 400+ mods",
					DownloadCount: 1500000,
					RefURL:        "https://curseforge.com/minecraft/modpacks/all-the-mods-9",
				},
			}, nil
		},
	})

	reqEmpty := httptest.NewRequest("GET", "/api/minecraft/wizard/modpacks/search?q=", nil)
	respEmpty, _ := app.Test(reqEmpty)
	bodyEmpty, _ := io.ReadAll(respEmpty.Body)
	if !strings.Contains(string(bodyEmpty), "Type a modpack name") {
		t.Errorf("expected prompt, got: %s", string(bodyEmpty))
	}

	// 3. Search error
	reqErr := httptest.NewRequest("GET", "/api/minecraft/wizard/modpacks/search?q=error", nil)
	respErr, _ := app.Test(reqErr)
	bodyErr, _ := io.ReadAll(respErr.Body)
	if !strings.Contains(string(bodyErr), "backend timeout") {
		t.Errorf("expected error message, got: %s", string(bodyErr))
	}

	// 4. No hits
	reqNone := httptest.NewRequest("GET", "/api/minecraft/wizard/modpacks/search?q=empty", nil)
	respNone, _ := app.Test(reqNone)
	bodyNone, _ := io.ReadAll(respNone.Body)
	if !strings.Contains(string(bodyNone), "No modpacks found") {
		t.Errorf("expected no modpacks found, got: %s", string(bodyNone))
	}

	// 5. Hits found
	reqHits := httptest.NewRequest("GET", "/api/minecraft/wizard/modpacks/search?q=atm9", nil)
	respHits, _ := app.Test(reqHits)
	bodyHits, _ := io.ReadAll(respHits.Body)
	if !strings.Contains(string(bodyHits), "All The Mods 9") {
		t.Errorf("expected pack name, got: %s", string(bodyHits))
	}
}

func TestMCWizardModsSearch(t *testing.T) {
	// 1. SearchMods unconfigured
	appNoCfg, _ := setupWizardApp(t, Config{})
	req1 := httptest.NewRequest("GET", "/api/minecraft/wizard/mods/search?q=jei", nil)
	resp1, _ := appNoCfg.Test(req1)
	body1, _ := io.ReadAll(resp1.Body)
	if !strings.Contains(string(body1), "Mod search unconfigured") {
		t.Errorf("expected unconfigured error, got: %s", string(body1))
	}

	// 2. Normal search
	app, _ := setupWizardApp(t, Config{
		SearchMods: func(ctx context.Context, query, mcVersion string) ([]ModHit, error) {
			if query == "err" {
				return nil, fmt.Errorf("mod search error")
			}
			if query == "empty" {
				return nil, nil
			}
			return []ModHit{
				{
					Slug:        "jei",
					Title:       "Just Enough Items",
					Description: "Item recipe viewer",
				},
			}, nil
		},
	})

	// Empty query
	reqEmpty := httptest.NewRequest("GET", "/api/minecraft/wizard/mods/search?q=", nil)
	respEmpty, _ := app.Test(reqEmpty)
	bodyEmpty, _ := io.ReadAll(respEmpty.Body)
	if !strings.Contains(string(bodyEmpty), "Search mods to populate results") {
		t.Errorf("expected prompt, got: %s", string(bodyEmpty))
	}

	// Error query
	reqErr := httptest.NewRequest("GET", "/api/minecraft/wizard/mods/search?q=err", nil)
	respErr, _ := app.Test(reqErr)
	bodyErr, _ := io.ReadAll(respErr.Body)
	if !strings.Contains(string(bodyErr), "mod search error") {
		t.Errorf("expected error, got: %s", string(bodyErr))
	}

	// No results
	reqNone := httptest.NewRequest("GET", "/api/minecraft/wizard/mods/search?q=empty", nil)
	respNone, _ := app.Test(reqNone)
	bodyNone, _ := io.ReadAll(respNone.Body)
	if !strings.Contains(string(bodyNone), "No mods found") {
		t.Errorf("expected no mods found, got: %s", string(bodyNone))
	}

	// Hits
	reqHits := httptest.NewRequest("GET", "/api/minecraft/wizard/mods/search?q=jei", nil)
	respHits, _ := app.Test(reqHits)
	bodyHits, _ := io.ReadAll(respHits.Body)
	if !strings.Contains(string(bodyHits), "Just Enough Items") {
		t.Errorf("expected mod hit title, got: %s", string(bodyHits))
	}
}

func TestMCWizardCartCheck(t *testing.T) {
	app, _ := setupWizardApp(t, Config{
		CheckCartCompat: func(ctx context.Context, slugs []string, mcVersion string) mccontent.CartCompatibility {
			return mccontent.CartCompatibility{
				BestLoader:   "neoforge",
				NeoForgeFit:  len(slugs),
				FabricFit:    len(slugs) - 1,
				TotalMods:    len(slugs),
			}
		},
	})

	req := httptest.NewRequest("POST", "/api/minecraft/wizard/cart/check", strings.NewReader(`{"cart":["jei","ferrite-core"],"mc_version":"1.21.1"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 OK, got: %v, err: %v", resp.StatusCode, err)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "bestLoader") || !strings.Contains(string(body), "jei") {
		t.Errorf("expected cart check signals and items, got: %s", string(body))
	}
}

func TestMCWizardCreateValidationAndCreation(t *testing.T) {
	app, instMgr := setupWizardApp(t, Config{
		ResolvePackRef: func(ctx context.Context, packRef string) string {
			return "https://curseforge.com/resolved/" + packRef
		},
		VerifyPackLoader: func(ctx context.Context, packID int, packName, requestedLoader string) string {
			return "fabric"
		},
	})

	// 1. Missing world name -> error toast
	reqNoName := httptest.NewRequest("POST", "/api/minecraft/wizard/create", strings.NewReader(`{"name":"","source":"vanilla"}`))
	reqNoName.Header.Set("Content-Type", "application/json")
	respNoName, _ := app.Test(reqNoName)
	bodyNoName, _ := io.ReadAll(respNoName.Body)
	if !strings.Contains(string(bodyNoName), "World name is required") {
		t.Errorf("expected name required error, got: %s", string(bodyNoName))
	}

	// 2. Assemble source without loader -> error toast
	reqNoLoader := httptest.NewRequest("POST", "/api/minecraft/wizard/create", strings.NewReader(`{"name":"My World","source":"assemble","loader":""}`))
	reqNoLoader.Header.Set("Content-Type", "application/json")
	respNoLoader, _ := app.Test(reqNoLoader)
	bodyNoLoader, _ := io.ReadAll(respNoLoader.Body)
	if !strings.Contains(string(bodyNoLoader), "select a mod loader") {
		t.Errorf("expected loader required error, got: %s", string(bodyNoLoader))
	}

	// 3. Successful create (vanilla)
	reqVanilla := httptest.NewRequest("POST", "/api/minecraft/wizard/create", strings.NewReader(`{
		"name": "Vanilla World",
		"source": "vanilla",
		"mc_version": "1.21.4",
		"tier": "small"
	}`))
	reqVanilla.Header.Set("Content-Type", "application/json")
	respVanilla, err := app.Test(reqVanilla)
	if err != nil || respVanilla.StatusCode != fiber.StatusOK {
		t.Fatalf("create vanilla world failed: %v", err)
	}
	bodyVanilla, _ := io.ReadAll(respVanilla.Body)
	if !strings.Contains(string(bodyVanilla), "/minecraft/provisioning/1") {
		t.Errorf("expected redirect to provisioning 1, got: %s", string(bodyVanilla))
	}

	// Verify instance exists in DB
	inst, err := instMgr.GetInstance(context.Background(), 1)
	if err != nil || inst == nil || inst.Name != "Vanilla World" {
		t.Errorf("instance 1 not created properly: %+v", inst)
	}

	// 4. Successful create (modpack with loader verification)
	reqPack := httptest.NewRequest("POST", "/api/minecraft/wizard/create", strings.NewReader(`{
		"name": "Cobblemon Adventure",
		"source": "modpack",
		"pack_name": "Cobblemon",
		"pack_ref": "cobblemon",
		"pack_id": "1234",
		"loader": "neoforge",
		"tier": "medium"
	}`))
	reqPack.Header.Set("Content-Type", "application/json")
	respPack, _ := app.Test(reqPack)
	bodyPack, _ := io.ReadAll(respPack.Body)
	if !strings.Contains(string(bodyPack), "/minecraft/provisioning/2") {
		t.Errorf("expected redirect to provisioning 2, got: %s", string(bodyPack))
	}

	inst2, err := instMgr.GetInstance(context.Background(), 2)
	if err != nil || inst2 == nil || inst2.Loader != "fabric" {
		t.Errorf("expected loader verified to fabric, got: %+v", inst2)
	}
}

func TestMCProvisioningPageAndStream(t *testing.T) {
	app, instMgr := setupWizardApp(t, Config{})

	// Seed instance
	_ = instMgr.SaveInstance(domain.Instance{
		GameID: domain.GameMinecraft,
		Number: 1,
		Name:   "Test Server",
		State:  domain.StateProvisioning,
	})

	// 1. Provisioning Page
	reqPage := httptest.NewRequest("GET", "/minecraft/provisioning/1", nil)
	respPage, err := app.Test(reqPage)
	if err != nil || respPage.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for provisioning page, got: %v", respPage.StatusCode)
	}

	// Non-existent instance
	reqNone := httptest.NewRequest("GET", "/minecraft/provisioning/99", nil)
	respNone, _ := app.Test(reqNone)
	if respNone.StatusCode != fiber.StatusNotFound {
		t.Errorf("expected 404 for non-existent instance, got: %d", respNone.StatusCode)
	}
}

func TestMCWizardImport(t *testing.T) {
	app, _ := setupWizardApp(t, Config{})

	// 1. Import without file fails (400 Bad Request)
	reqNoFile := httptest.NewRequest("POST", "/api/minecraft/wizard/import", strings.NewReader(`name=ImportedWorld&tier=small`))
	reqNoFile.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respNoFile, _ := app.Test(reqNoFile)
	if respNoFile.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for missing file, got: %d", respNoFile.StatusCode)
	}
	bodyNoFile, _ := io.ReadAll(respNoFile.Body)
	if !strings.Contains(string(bodyNoFile), "No file uploaded") {
		t.Errorf("expected No file uploaded error, got: %s", string(bodyNoFile))
	}

	// 2. Import valid txt file
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	part, err := w.CreateFormFile("file", "modlist.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("jei\nferrite-core\n"))
	_ = w.Close()

	reqValid := httptest.NewRequest("POST", "/api/minecraft/wizard/import", &b)
	reqValid.Header.Set("Content-Type", w.FormDataContentType())
	respValid, err := app.Test(reqValid)
	if err != nil || respValid.StatusCode != fiber.StatusOK {
		t.Fatalf("import valid modlist failed: %v", err)
	}
	bodyValid, _ := io.ReadAll(respValid.Body)
	if !strings.Contains(string(bodyValid), "Imported Modded World") || !strings.Contains(string(bodyValid), "jei") {
		t.Errorf("expected Imported Modded World in response, got: %s", string(bodyValid))
	}
}

func TestActorFallback(t *testing.T) {
	app := fiber.New()
	h := New(Config{})
	app.Get("/test-actor", func(c *fiber.Ctx) error {
		return c.SendString(h.cfg.Actor(c))
	})
	app.Get("/test-actor-custom", func(c *fiber.Ctx) error {
		c.Locals("actor", "custom-admin")
		return c.SendString(h.cfg.Actor(c))
	})
	app.Get("/test-actor-empty", func(c *fiber.Ctx) error {
		c.Locals("actor", "")
		return c.SendString(h.cfg.Actor(c))
	})

	req1 := httptest.NewRequest("GET", "/test-actor", nil)
	resp1, _ := app.Test(req1)
	b1, _ := io.ReadAll(resp1.Body)
	if string(b1) != "local" {
		t.Errorf("expected local, got %s", b1)
	}

	req2 := httptest.NewRequest("GET", "/test-actor-custom", nil)
	resp2, _ := app.Test(req2)
	b2, _ := io.ReadAll(resp2.Body)
	if string(b2) != "custom-admin" {
		t.Errorf("expected custom-admin, got %s", b2)
	}

	req3 := httptest.NewRequest("GET", "/test-actor-empty", nil)
	resp3, _ := app.Test(req3)
	b3, _ := io.ReadAll(resp3.Body)
	if string(b3) != "local" {
		t.Errorf("expected local, got %s", b3)
	}
}

func TestMCWizardPageErrorsAndFallbacks(t *testing.T) {
	// Fallback to default versions when VersionReleases is nil
	appDefault, _ := setupWizardApp(t, Config{})
	req1 := httptest.NewRequest("GET", "/minecraft/create", nil)
	resp1, err := appDefault.Test(req1)
	if err != nil || resp1.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got: %v", resp1.StatusCode)
	}
	body1, _ := io.ReadAll(resp1.Body)
	if !strings.Contains(string(body1), "1.21.1") {
		t.Errorf("expected default 1.21.1 in page, got: %s", body1)
	}

	// Fallback when VersionReleases returns empty slice
	appEmptyRel, _ := setupWizardApp(t, Config{
		VersionReleases: func(ctx context.Context, limit int) []string {
			return nil
		},
	})
	req2 := httptest.NewRequest("GET", "/minecraft/create", nil)
	resp2, err := appEmptyRel.Test(req2)
	if err != nil || resp2.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got: %v", resp2.StatusCode)
	}

	// ListInstances error when store is closed
	appErr, _, st := setupWizardAppWithStore(t, Config{})
	_ = st.Close()
	reqErr := httptest.NewRequest("GET", "/minecraft/create", nil)
	respErr, err := appErr.Test(reqErr)
	if err != nil || respErr.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500 on closed db, got: %v", respErr.StatusCode)
	}
}

func TestSSEPatchElementsError(t *testing.T) {
	orig := innerElement
	defer func() { innerElement = orig }()
	innerElement = func(w *bufio.Writer, selector, content string) error {
		return errors.New("forced element patch error")
	}

	app, _ := setupWizardApp(t, Config{})
	req := httptest.NewRequest("GET", "/api/minecraft/wizard/modpacks/search?q=test", nil)
	resp, err := app.Test(req)
	if err == nil && resp.StatusCode == fiber.StatusOK {
		t.Fatalf("expected error from ssePatchElements, got status: %v", resp.StatusCode)
	}
}

func TestMCWizardCartCheckVariants(t *testing.T) {
	// 1. CheckCartCompat is nil fallback, and empty strings in cart
	appNilCompat, _ := setupWizardApp(t, Config{CheckCartCompat: nil})
	reqJSON := httptest.NewRequest("POST", "/api/minecraft/wizard/cart/check", strings.NewReader(`{"cart":["", " ", "jei"],"mc_version":"1.21.1"}`))
	reqJSON.Header.Set("Content-Type", "application/json")
	respJSON, err := appNilCompat.Test(reqJSON)
	if err != nil || respJSON.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 OK, got: %v", respJSON.StatusCode)
	}
	bodyJSON, _ := io.ReadAll(respJSON.Body)
	if !strings.Contains(string(bodyJSON), "bestLoader") || !strings.Contains(string(bodyJSON), "neoforge") {
		t.Errorf("expected neoforge fallback, got: %s", bodyJSON)
	}

	// 2. BodyParser fallback with form values (e.g. invalid JSON)
	reqForm := httptest.NewRequest("POST", "/api/minecraft/wizard/cart/check?cart=modA,modB&mc_version=1.20.1", strings.NewReader("{invalid-json"))
	reqForm.Header.Set("Content-Type", "application/json")
	respForm, err := appNilCompat.Test(reqForm)
	if err != nil || respForm.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 OK for form fallback, got: %v", respForm.StatusCode)
	}

	// 3. patchSignals error
	origSignals := patchSignals
	patchSignals = func(w *bufio.Writer, signals any) error {
		return errors.New("forced patchSignals error")
	}
	reqErrSig := httptest.NewRequest("POST", "/api/minecraft/wizard/cart/check", strings.NewReader(`{"cart":["jei"]}`))
	reqErrSig.Header.Set("Content-Type", "application/json")
	respErrSig, _ := appNilCompat.Test(reqErrSig)
	if respErrSig.StatusCode == fiber.StatusOK {
		t.Errorf("expected non-OK on patchSignals error")
	}
	patchSignals = origSignals

	// 4. innerElement error in ssePatch
	origInner := innerElement
	innerElement = func(w *bufio.Writer, selector, content string) error {
		return errors.New("forced innerElement error")
	}
	reqErrInner := httptest.NewRequest("POST", "/api/minecraft/wizard/cart/check", strings.NewReader(`{"cart":["jei"]}`))
	reqErrInner.Header.Set("Content-Type", "application/json")
	respErrInner, _ := appNilCompat.Test(reqErrInner)
	if respErrInner.StatusCode == fiber.StatusOK {
		t.Errorf("expected non-OK on innerElement error")
	}
	innerElement = origInner
}

func TestMCWizardCreateFormAndVariants(t *testing.T) {
	// 1. MCInstances is nil -> error toast
	appNilInst := fiber.New()
	hNil := New(Config{})
	hNil.Register(appNilInst)
	reqNil := httptest.NewRequest("POST", "/api/minecraft/wizard/create", strings.NewReader(`{"name":"World"}`))
	reqNil.Header.Set("Content-Type", "application/json")
	respNil, _ := appNilInst.Test(reqNil)
	bNil, _ := io.ReadAll(respNil.Body)
	if !strings.Contains(string(bNil), "Instance manager not configured") {
		t.Errorf("expected unconfigured toast, got: %s", bNil)
	}

	// 2. Form value parsing & fallbacks
	var resolvedRef string
	var verifiedID int
	app, instMgr := setupWizardApp(t, Config{
		ResolvePackRef: func(ctx context.Context, packRef string) string {
			resolvedRef = "https://resolved.com/" + packRef
			return resolvedRef
		},
		VerifyPackLoader: func(ctx context.Context, packID int, packName, requestedLoader string) string {
			verifiedID = packID
			return "neoforge"
		},
	})

	formVals := url.Values{
		"name":          {"FormWorld"},
		"source":        {"modpack"},
		"loader":        {"fabric"},
		"mc_version":    {""}, // empty -> fallback to 1.21.1
		"tier":          {"small"},
		"seed":          {"987654"},
		"motd":          {"My MOTD"},
		"difficulty":    {"hard"},
		"gamemode":      {"survival"},
		"world_type":    {"flat"},
		"pack_name":     {"Cobble"},
		"pack_ref":      {"cobble-ref"},
		"pack_provider": {"modrinth"},
		"pack_id":       {"555"},
		"raw_mods":      {"jei\nappleskin"},
	}
	reqForm := httptest.NewRequest("POST", "/api/minecraft/wizard/create", strings.NewReader(formVals.Encode()))
	reqForm.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respForm, err := app.Test(reqForm)
	if err != nil || respForm.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for form create, got: %v, err: %v", respForm.StatusCode, err)
	}
	if resolvedRef != "https://resolved.com/cobble-ref" {
		t.Errorf("expected resolved ref, got: %s", resolvedRef)
	}
	if verifiedID != 555 {
		t.Errorf("expected pack ID 555, got: %d", verifiedID)
	}
	inst, err := instMgr.GetInstance(context.Background(), 1)
	if err != nil || inst == nil {
		t.Fatalf("instance 1 not found: %v", err)
	}
	if inst.Pack == nil || inst.Pack.Provider != domain.ProviderModrinth {
		t.Errorf("expected ProviderModrinth, got: %+v", inst.Pack)
	}
	if inst.MCVersion != "1.21.1" {
		t.Errorf("expected default 1.21.1, got: %s", inst.MCVersion)
	}

	// 3. Assemble source with missing loader via FormValue
	formNoLoader := url.Values{
		"name":   {"NoLoaderWorld"},
		"source": {"assemble"},
		"loader": {""},
	}
	reqNoLoader := httptest.NewRequest("POST", "/api/minecraft/wizard/create", strings.NewReader(formNoLoader.Encode()))
	reqNoLoader.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respNoLoader, _ := app.Test(reqNoLoader)
	bNoLoader, _ := io.ReadAll(respNoLoader.Body)
	if !strings.Contains(string(bNoLoader), "select a mod loader") {
		t.Errorf("expected select mod loader error, got: %s", bNoLoader)
	}

	// 4. Cart as []any with non-string and empty string
	reqAnyCart := httptest.NewRequest("POST", "/api/minecraft/wizard/create", strings.NewReader(`{
		"name": "AnyCartWorld",
		"source": "assemble",
		"loader": "fabric",
		"cart": ["jei", "", 123, "cloth-config"]
	}`))
	reqAnyCart.Header.Set("Content-Type", "application/json")
	respAnyCart, err := app.Test(reqAnyCart)
	if err != nil || respAnyCart.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for any cart, got: %v", respAnyCart.StatusCode)
	}

	// 5. Cart as string (comma-separated)
	reqStrCart := httptest.NewRequest("POST", "/api/minecraft/wizard/create", strings.NewReader(`{
		"name": "StrCartWorld",
		"source": "assemble",
		"loader": "fabric",
		"cart": "jei,appleskin"
	}`))
	reqStrCart.Header.Set("Content-Type", "application/json")
	respStrCart, err := app.Test(reqStrCart)
	if err != nil || respStrCart.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for str cart, got: %v", respStrCart.StatusCode)
	}

	// 6. Cart from FormValue
	formCart := url.Values{
		"name":   {"FormCartWorld"},
		"source": {"assemble"},
		"loader": {"neoforge"},
		"cart":   {"mod1,mod2"},
	}
	reqFormCart := httptest.NewRequest("POST", "/api/minecraft/wizard/create", strings.NewReader(formCart.Encode()))
	reqFormCart.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respFormCart, err := app.Test(reqFormCart)
	if err != nil || respFormCart.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for form cart, got: %v", respFormCart.StatusCode)
	}

	// 7. Modpack with CurseForge provider, invalid pack_id ("not-an-int")
	reqCF := httptest.NewRequest("POST", "/api/minecraft/wizard/create", strings.NewReader(`{
		"name": "CFWorld",
		"source": "modpack",
		"loader": "neoforge",
		"pack_name": "CFPack",
		"pack_ref": "cf-ref",
		"pack_provider": "curseforge",
		"pack_id": "invalid-int"
	}`))
	reqCF.Header.Set("Content-Type", "application/json")
	respCF, err := app.Test(reqCF)
	if err != nil || respCF.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for CF modpack, got: %v", respCF.StatusCode)
	}

	// 8. CreateInstance failure on closed db
	appFail, _, stFail := setupWizardAppWithStore(t, Config{})
	_ = stFail.Close()
	reqFail := httptest.NewRequest("POST", "/api/minecraft/wizard/create", strings.NewReader(`{"name":"FailWorld","source":"vanilla"}`))
	reqFail.Header.Set("Content-Type", "application/json")
	respFail, _ := appFail.Test(reqFail)
	bFail, _ := io.ReadAll(respFail.Body)
	if !strings.Contains(string(bFail), "Failed to create world") {
		t.Errorf("expected Failed to create world toast, got: %s", bFail)
	}

	// 9. Source fallback to FormValue when req.Source is empty in JSON
	reqSrcFallback := httptest.NewRequest("POST", "/api/minecraft/wizard/create?source=vanilla", strings.NewReader(`{"name":"SrcFallbackWorld"}`))
	reqSrcFallback.Header.Set("Content-Type", "application/json")
	respSrcFallback, err := app.Test(reqSrcFallback)
	if err != nil || respSrcFallback.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for src fallback, got: %v", respSrcFallback.StatusCode)
	}

	// 10. PackID fallback to FormValue when req.PackID is empty in JSON
	reqPackIDFallback := httptest.NewRequest("POST", "/api/minecraft/wizard/create?pack_id=999", strings.NewReader(`{
		"name": "PackIDFallbackWorld",
		"source": "modpack",
		"pack_name": "MyPack",
		"loader": "fabric"
	}`))
	reqPackIDFallback.Header.Set("Content-Type", "application/json")
	respPackIDFallback, err := app.Test(reqPackIDFallback)
	if err != nil || respPackIDFallback.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for packID fallback, got: %v", respPackIDFallback.StatusCode)
	}

	// 11. Multi-value cart form fields
	multiCart := url.Values{
		"name":   {"MultiValCartWorld"},
		"source": {"assemble"},
		"loader": {"fabric"},
		"cart":   {"itemA", "itemB"},
	}
	reqMultiCart := httptest.NewRequest("POST", "/api/minecraft/wizard/create", strings.NewReader(multiCart.Encode()))
	reqMultiCart.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respMultiCart, err := app.Test(reqMultiCart)
	if err != nil || respMultiCart.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for multi-val cart, got: %v", respMultiCart.StatusCode)
	}

	// 12. Cart as []string
	origBP := bodyParser
	defer func() { bodyParser = origBP }()
	bodyParser = func(c *fiber.Ctx, out any) error {
		_ = c.BodyParser(out)
		if r, ok := out.(*wizardCreateReq); ok {
			r.Cart = []string{"itemX", "itemY"}
		}
		return nil
	}
	reqSliceCart := httptest.NewRequest("POST", "/api/minecraft/wizard/create", strings.NewReader(`{"name":"SliceCartWorld","source":"assemble","loader":"fabric"}`))
	reqSliceCart.Header.Set("Content-Type", "application/json")
	respSliceCart, err := app.Test(reqSliceCart)
	if err != nil || respSliceCart.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for slice cart, got: %v", respSliceCart.StatusCode)
	}
	bodyParser = origBP
}

func TestMCProvisioningStreamCases(t *testing.T) {
	// 1. MCInstances is nil -> 200 SSE immediate
	appNil := fiber.New()
	hNil := New(Config{})
	hNil.Register(appNil)
	reqNil := httptest.NewRequest("GET", "/api/minecraft/provisioning/1/stream", nil)
	respNil, err := appNil.Test(reqNil)
	if err != nil || respNil.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got: %v", respNil.StatusCode)
	}
	bNil, _ := io.ReadAll(respNil.Body)
	if !strings.Contains(string(bNil), "ready") {
		t.Errorf("expected ready signals, got: %s", bNil)
	}

	// 2. MCProvisioningPage invalid number & MCInstances nil
	reqPageNil := httptest.NewRequest("GET", "/minecraft/provisioning/1", nil)
	respPageNil, _ := appNil.Test(reqPageNil)
	if respPageNil.StatusCode != fiber.StatusServiceUnavailable {
		t.Errorf("expected 503, got: %d", respPageNil.StatusCode)
	}

	app, instMgr := setupWizardApp(t, Config{})
	reqInvalidPage := httptest.NewRequest("GET", "/minecraft/provisioning/not-a-number", nil)
	respInvalidPage, _ := app.Test(reqInvalidPage)
	if respInvalidPage.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for invalid page num, got: %d", respInvalidPage.StatusCode)
	}

	// 3. MCProvisioningStream invalid number
	reqInvalidStream := httptest.NewRequest("GET", "/api/minecraft/provisioning/not-a-number/stream", nil)
	respInvalidStream, _ := app.Test(reqInvalidStream)
	if respInvalidStream.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for invalid stream num, got: %d", respInvalidStream.StatusCode)
	}

	// 4. MCProvisioningStream not found
	reqNoneStream := httptest.NewRequest("GET", "/api/minecraft/provisioning/999/stream", nil)
	respNoneStream, _ := app.Test(reqNoneStream)
	if respNoneStream.StatusCode != fiber.StatusNotFound {
		t.Errorf("expected 404 for missing instance stream, got: %d", respNoneStream.StatusCode)
	}

	// 5. Streaming loop execution: initial ready break
	_ = instMgr.SaveInstance(domain.Instance{
		GameID: domain.GameMinecraft,
		Number: 10,
		Name:   "Stream Server",
		State:  domain.StateProvisioning,
	})
	origSleep := sleep
	defer func() { sleep = origSleep }()
	sleep = func(d time.Duration) {}

	reqStream := httptest.NewRequest("GET", "/api/minecraft/provisioning/10/stream", nil)
	respStream, err := app.Test(reqStream)
	if err != nil || respStream.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for stream, got: %v", respStream.StatusCode)
	}
	bodyStream, err := io.ReadAll(respStream.Body)
	if err != nil || !strings.Contains(string(bodyStream), `"ready":true`) {
		t.Errorf("expected stream ready signal, got: %s, err: %v", bodyStream, err)
	}

	// 6. Streaming loop error branch (ProvisioningStatus error recovery)
	_ = instMgr.SaveInstance(domain.Instance{
		GameID: domain.GameMinecraft,
		Number: 11,
		Name:   "Stream Server Err",
		State:  domain.StateProvisioning,
	})
	origStatus := provisioningStatus
	defer func() { provisioningStatus = origStatus }()
	var attempts int
	provisioningStatus = func(m *instances.InstanceManager, ctx context.Context, num int) (string, bool, error) {
		attempts++
		if attempts == 1 {
			return "", false, errors.New("simulated status error")
		}
		return "ready", true, nil
	}

	reqStreamErr := httptest.NewRequest("GET", "/api/minecraft/provisioning/11/stream", nil)
	respStreamErr, err := app.Test(reqStreamErr)
	if err != nil || respStreamErr.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for stream err, got: %v", respStreamErr.StatusCode)
	}
	bodyStreamErr, _ := io.ReadAll(respStreamErr.Body)
	if !strings.Contains(string(bodyStreamErr), `"syncing"`) {
		t.Errorf("expected syncing signal in stream error recovery, got: %s", bodyStreamErr)
	}
}

func makeTestZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(content)
	}
	_ = zw.Close()
	return b.Bytes()
}

func TestMCWizardImportVariants(t *testing.T) {
	app, _ := setupWizardApp(t, Config{})

	sendUpload := func(filename string, data []byte) ([]byte, int) {
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		part, err := w.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(data)
		_ = w.Close()

		req := httptest.NewRequest("POST", "/api/minecraft/wizard/import", &b)
		req.Header.Set("Content-Type", w.FormDataContentType())
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		return body, resp.StatusCode
	}

	// 1. Valid mrpack with modrinth.index.json
	mrpackIndex := `{
		"name": "My Mrpack",
		"dependencies": {
			"minecraft": "1.21.1",
			"fabric-loader": "0.16.0"
		},
		"files": [
			{"path": "mods/jei-1.21.1.jar"}
		]
	}`
	mrpackData := makeTestZip(t, map[string][]byte{
		"modrinth.index.json": []byte(mrpackIndex),
	})
	b1, code1 := sendUpload("mypack.mrpack", mrpackData)
	if code1 != fiber.StatusOK || !strings.Contains(string(b1), "My Mrpack") {
		t.Errorf("expected 200 for valid mrpack, got: %d, body: %s", code1, b1)
	}

	// 2. Corrupt mrpack
	bCorruptMr, codeCorruptMr := sendUpload("bad.mrpack", []byte("corrupt-bytes"))
	if codeCorruptMr != fiber.StatusBadRequest || !strings.Contains(string(bCorruptMr), "Failed to parse archive") {
		t.Errorf("expected 400 for corrupt mrpack, got: %d, body: %s", codeCorruptMr, bCorruptMr)
	}

	// 3. Zip with modrinth.index.json
	bZipMr, codeZipMr := sendUpload("mypack.zip", mrpackData)
	if codeZipMr != fiber.StatusOK || !strings.Contains(string(bZipMr), "My Mrpack") {
		t.Errorf("expected 200 for zip mrpack, got: %d, body: %s", codeZipMr, bZipMr)
	}

	// 4. Zip with Prism/MMC format (mmc-pack.json)
	mmcPack := `{
		"components": [
			{"uid": "net.minecraft", "version": "1.20.1"},
			{"uid": "net.fabricmc.fabric-loader", "version": "0.15.0"}
		]
	}`
	prismZipData := makeTestZip(t, map[string][]byte{
		"mmc-pack.json":             []byte(mmcPack),
		"instance.cfg":              []byte("name=Prism World\n"),
		"mods/appleskin-1.20.1.jar": []byte("dummy"),
	})
	bPrism, codePrism := sendUpload("prism.zip", prismZipData)
	if codePrism != fiber.StatusOK || !strings.Contains(string(bPrism), "Prism World") {
		t.Errorf("expected 200 for prism zip, got: %d, body: %s", codePrism, bPrism)
	}

	// 5. Corrupt zip
	bBadZip, codeBadZip := sendUpload("bad.zip", []byte("corrupt-zip-bytes"))
	if codeBadZip != fiber.StatusBadRequest {
		t.Errorf("expected 400 for corrupt zip, got: %d, body: %s", codeBadZip, bBadZip)
	}

	// 6. Unknown extension: mrpack fallback
	bUnkMr, codeUnkMr := sendUpload("unknown.bundle", mrpackData)
	if codeUnkMr != fiber.StatusOK || !strings.Contains(string(bUnkMr), "My Mrpack") {
		t.Errorf("expected 200 for unknown ext mrpack, got: %d, body: %s", codeUnkMr, bUnkMr)
	}

	// 7. Unknown extension: prism fallback
	bUnkPrism, codeUnkPrism := sendUpload("unknown.bundle", prismZipData)
	if codeUnkPrism != fiber.StatusOK || !strings.Contains(string(bUnkPrism), "Prism World") {
		t.Errorf("expected 200 for unknown ext prism, got: %d, body: %s", codeUnkPrism, bUnkPrism)
	}

	// 8. Unknown extension: text fallback
	bUnkTxt, codeUnkTxt := sendUpload("custom.data", []byte("jei\nappleskin\n"))
	if codeUnkTxt != fiber.StatusOK || !strings.Contains(string(bUnkTxt), "Imported Modded World") {
		t.Errorf("expected 200 for unknown ext text, got: %d, body: %s", codeUnkTxt, bUnkTxt)
	}

	// 9. Mrpack with empty name -> fallback to file basename
	emptyNameMrpack := makeTestZip(t, map[string][]byte{
		"modrinth.index.json": []byte(`{"name":"","dependencies":{"minecraft":"1.21.1","neoforge":"1.0"}}`),
	})
	bEmptyName, codeEmptyName := sendUpload("my-cool-pack.mrpack", emptyNameMrpack)
	if codeEmptyName != fiber.StatusOK || !strings.Contains(string(bEmptyName), "my-cool-pack") {
		t.Errorf("expected filename fallback for empty name, got: %d, body: %s", codeEmptyName, bEmptyName)
	}

	// 10. openFile error seam
	origOpen := openFile
	defer func() { openFile = origOpen }()
	openFile = func(fh *multipart.FileHeader) (multipart.File, error) {
		return nil, errors.New("open error")
	}
	bOpenErr, codeOpenErr := sendUpload("test.txt", []byte("hello"))
	if codeOpenErr != fiber.StatusInternalServerError || !strings.Contains(string(bOpenErr), "Failed to open file") {
		t.Errorf("expected 500 for openFile error, got: %d, body: %s", codeOpenErr, bOpenErr)
	}
	openFile = origOpen

	// 11. readAll error seam
	origRead := readAll
	defer func() { readAll = origRead }()
	readAll = func(r io.Reader) ([]byte, error) {
		return nil, errors.New("read error")
	}
	bReadErr, codeReadErr := sendUpload("test.txt", []byte("hello"))
	if codeReadErr != fiber.StatusInternalServerError || !strings.Contains(string(bReadErr), "Failed to read file") {
		t.Errorf("expected 500 for readAll error, got: %d, body: %s", codeReadErr, bReadErr)
	}
	readAll = origRead

	// 12. parseMrpack returns (nil, nil) -> covers world == nil branch
	origParseMr := parseMrpack
	defer func() { parseMrpack = origParseMr }()
	parseMrpack = func(r io.ReaderAt, size int64) (*modpack.ImportedWorld, error) {
		return nil, nil
	}
	bNilWorld, codeNilWorld := sendUpload("empty.mrpack", []byte("dummy"))
	if codeNilWorld != fiber.StatusBadRequest || !strings.Contains(string(bNilWorld), "Unable to extract world data") {
		t.Errorf("expected 400 for nil world, got: %d, body: %s", codeNilWorld, bNilWorld)
	}
	parseMrpack = origParseMr
}
