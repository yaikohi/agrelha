package wizard

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	mccontent "agrelha/internal/app/content"
	"agrelha/internal/app/instances"
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

func setupWizardApp(t *testing.T, cfg Config) (*fiber.App, *instances.InstanceManager) {
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
	return app, instMgr
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
