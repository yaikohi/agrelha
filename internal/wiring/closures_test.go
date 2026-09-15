package wiring

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakek8s "k8s.io/client-go/kubernetes/fake"

	"agrelha/internal/app/instances"
	"agrelha/internal/app/modupdates"
	"agrelha/internal/domain"
	"agrelha/internal/infra/content/modpackindex"
	"agrelha/internal/infra/content/modrinth"
	"agrelha/internal/infra/content/thunderstore"
	"agrelha/internal/infra/kube"
	k8sruntime "agrelha/internal/infra/runtime/k8s"
	"agrelha/internal/infra/store"
	"agrelha/internal/platform/config"
	"agrelha/internal/ports"
	minecrafthttp "agrelha/internal/web/handlers/minecraft"
	valheimhttp "agrelha/internal/web/handlers/valheim"
)

func TestActorHelper(t *testing.T) {
	app := fiber.New()
	var gotDefault, gotCustom string

	app.Get("/default", func(c *fiber.Ctx) error {
		gotDefault = actor(c)
		return c.SendStatus(200)
	})
	app.Get("/custom", func(c *fiber.Ctx) error {
		c.Locals("actor", "custom-admin")
		gotCustom = actor(c)
		return c.SendStatus(200)
	})

	req1 := httptest.NewRequest(fiber.MethodGet, "/default", nil)
	_, _ = app.Test(req1)
	if gotDefault != "local" {
		t.Errorf("actor default = %q, want 'local'", gotDefault)
	}

	req2 := httptest.NewRequest(fiber.MethodGet, "/custom", nil)
	_, _ = app.Test(req2)
	if gotCustom != "custom-admin" {
		t.Errorf("actor custom = %q, want 'custom-admin'", gotCustom)
	}
}

func TestWiring_WizardEndpoints(t *testing.T) {
	// Mock ModpackIndex server covering all pack link variations
	mpiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/modpacks") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"id":      1,
						"name":    "Pack 1",
						"summary": "Sum 1",
						"links":   map[string]string{"curseforge": "https://curseforge.com/pack1"},
					},
					{
						"id":      2,
						"name":    "Pack 2",
						"summary": "Sum 2",
						"url":     "https://curseforge.com/pack2",
					},
					{
						"id":      3,
						"name":    "Pack 3",
						"summary": "Sum 3",
						"url":     "https://other.com/pack3",
					},
					{
						"id":       4,
						"name":     "Pack 4",
						"summary":  "Sum 4",
						"page_url": "https://page.com/pack4",
					},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/modpack/10/mods") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"name": "jei",
						"modrinth_info": []map[string]any{
							{"loaders": []string{"neoforge"}},
						},
					},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/modpack/20/mods") {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
			return
		}
		if strings.Contains(r.URL.Path, "/modpack/10") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"id":    10,
					"links": map[string]string{"curseforge": "https://curseforge.com/pack10"},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/modpack/11") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"id":  11,
					"url": "https://curseforge.com/pack11",
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/modpack/12") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"id":       12,
					"page_url": "https://other.com/pack12",
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer mpiSrv.Close()

	// Mock Modrinth server
	mrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/v2/search") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hits": []map[string]any{
					{"slug": "jei", "title": "Just Enough Items", "description": "desc", "icon_url": "http://img"},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/v2/project/") && strings.HasSuffix(r.URL.Path, "/version") {
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"game_versions": []string{"1.21.1"},
					"loaders":       []string{"neoforge"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer mrSrv.Close()

	d := Deps{
		MPI: modpackindex.New(mpiSrv.URL),
		MR:  modrinth.New(mrSrv.URL),
	}

	h := buildWizardHandler(d)
	app := fiber.New()
	h.Register(app)

	// 1. Modpack search
	f1 := url.Values{"packQuery": {"all the mods"}}
	req1 := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/modpacks/search", strings.NewReader(f1.Encode()))
	req1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp1, err := app.Test(req1)
	if err != nil || resp1.StatusCode != 200 {
		t.Fatalf("modpacks search failed: %v", err)
	}

	// 2. Mod search
	f2 := url.Values{"modQuery": {"jei"}, "version": {"1.21.1"}}
	req2 := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/mods/search", strings.NewReader(f2.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp2, err := app.Test(req2)
	if err != nil || resp2.StatusCode != 200 {
		t.Fatalf("mods search failed: %v", err)
	}

	// 3. Cart check
	req3 := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/cart/check", strings.NewReader(`{"cart":["jei"],"mc_version":"1.21.1"}`))
	req3.Header.Set("Content-Type", "application/json")
	resp3, err := app.Test(req3)
	if err != nil || resp3.StatusCode != 200 {
		t.Fatalf("cart check failed: %v", err)
	}

	// 4. Wizard create with resolvePackRef variations and verifyPackLoader
	for _, packRef := range []string{
		"https://modpackindex.com/modpack/10/pack10",
		"https://modpackindex.com/modpack/11/pack11",
		"https://modpackindex.com/modpack/12/pack12",
		"https://modpackindex.com/modpack/invalid/pack",
		"https://curseforge.com/modpack/direct",
	} {
		f := url.Values{
			"name":          {"Pack Test"},
			"source":        {"modpack"},
			"pack_id":       {"10"},
			"pack_name":     {"Pack 10"},
			"pack_ref":      {packRef},
			"pack_provider": {"curseforge"},
			"loader":        {"fabric"},
			"mc_version":    {"1.21.1"},
		}
		req := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/create", strings.NewReader(f.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_, _ = app.Test(req)
	}

	// Test verifyPackLoader with empty mods (pack 20)
	fEmpty := url.Values{
		"name":          {"Empty Pack"},
		"source":        {"modpack"},
		"pack_id":       {"20"},
		"pack_name":     {"Pack 20"},
		"pack_ref":      {"https://modpackindex.com/modpack/20/pack20"},
		"pack_provider": {"curseforge"},
		"loader":        {"fabric"},
		"mc_version":    {"1.21.1"},
	}
	reqEmpty := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/create", strings.NewReader(fEmpty.Encode()))
	reqEmpty.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, _ = app.Test(reqEmpty)
}

func TestWiring_DashboardEndpoints(t *testing.T) {
	tempDir := t.TempDir()
	st, err := store.Open(filepath.Join(tempDir, "dash.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	vhChecker := modupdates.New(nil, nil, modupdates.WithRestorePoints(st))
	mcChecker := modupdates.New(nil, nil, modupdates.WithRestorePoints(st))

	d := Deps{
		Store:        st,
		ModUpdates:   vhChecker,
		MCModUpdates: mcChecker,
	}

	cfg := &config.Config{
		GrafanaDashboardURL: "http://grafana.local",
		ValheimAddress:      "192.168.20.224",
		GameNodeName:        "game-01",
	}

	mcH := minecrafthttp.New(minecrafthttp.Config{})
	vhH := valheimhttp.New(valheimhttp.Config{})

	h := buildDashboardHandler(cfg, d, nil, mcH, nil, vhH)
	app := fiber.New()
	app.Get("/", h.DashboardPage)
	app.Get("/sse", h.SSEMain)

	req1 := httptest.NewRequest(fiber.MethodGet, "/", nil)
	resp1, err := app.Test(req1)
	if err != nil || resp1.StatusCode != 200 {
		t.Errorf("dashboard page failed: %v, status: %d", err, resp1.StatusCode)
	}

	sseCtx, sseCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer sseCancel()
	req2 := httptest.NewRequest(fiber.MethodGet, "/sse", nil).WithContext(sseCtx)
	resp2, err := app.Test(req2, 200)
	if err == nil && resp2 != nil {
		_ = resp2.Body.Close()
	}
}

func TestWiring_BuildContentAndMinecraftHandlers(t *testing.T) {
	cs := fakek8s.NewSimpleClientset(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim-mod-configs", Namespace: "valheim"},
			Data:       map[string]string{"foo.cfg": "bar=1\n"},
		},
	)
	k8sClient := k8s.NewWithClientset(cs, "valheim", "valheim")
	d := Deps{
		K8s:   k8sClient,
		MCK8s: k8sClient,
	}

	contentH := buildContentHandler(&config.Config{ModConfigsPath: "valheim/configs"}, d, nil)
	app := fiber.New()
	contentH.RegisterProtected(app)

	// Exercise ConfigData by calling /configs
	reqConfigs := httptest.NewRequest(fiber.MethodGet, "/configs", nil)
	_, _ = app.Test(reqConfigs)

	// Test Minecraft handler search mods callback
	mrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"hits": []map[string]any{
				{"slug": "sodium", "title": "Sodium", "description": "Renderer", "icon_url": "http://img"},
			},
		})
	}))
	defer mrSrv.Close()

	d.MR = modrinth.New(mrSrv.URL)
	mcH := buildMinecraftHandler(&config.Config{BackupsDir: t.TempDir()}, d, nil)
	mcApp := fiber.New()
	mcH.RegisterProtected(mcApp)

	req := httptest.NewRequest(fiber.MethodGet, "/api/minecraft/1/mods/search?q=sodium&version=1.21.1&loader=fabric", nil)
	resp, err := mcApp.Test(req)
	if err != nil || resp.StatusCode != 200 {
		t.Errorf("mods search failed: %v, status: %v", err, resp.StatusCode)
	}
}

func TestWiring_InstanceManagerClosures(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "inst_closures.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cs := fakek8s.NewSimpleClientset(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-inst-01-01-mods", Namespace: "minecraft-modded"},
			Data: map[string]string{
				"mods.txt": "jei\nappleskin\n# comment\n",
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-inst-01-01-configs", Namespace: "minecraft-modded"},
			Data: map[string]string{
				"server.properties": "difficulty=hard\n",
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "minecraft-modded-configs", Namespace: "minecraft-modded"},
			Data: map[string]string{
				"global.json": `{"test":true}`,
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim-valheim-01-01-mods", Namespace: "valheim"},
			Data: map[string]string{
				"mods.txt": "mod-a\nmod-b\n",
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim-valheim-01-01-configs", Namespace: "valheim"},
			Data: map[string]string{
				"valheim.cfg": "key=val\n",
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim-mod-configs", Namespace: "valheim"},
			Data: map[string]string{
				"global_valheim.cfg": "enabled=true\n",
			},
		},
	)

	origK8sClient := newK8sClient
	defer func() { newK8sClient = origK8sClient }()
	newK8sClient = func(ns, dep string) (*k8s.Client, error) {
		return k8s.NewWithClientset(cs, ns, dep), nil
	}

	cfg := &config.Config{
		DBPath:              dbPath,
		MinecraftDeployment: "minecraft-modded",
		MinecraftNamespace:  "minecraft-modded",
		ValheimDeployment:   "valheim",
		ValheimNamespace:    "valheim",
	}

	deps, err := Build(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Seed instances
	_ = deps.MCInstances.SaveInstance(domain.Instance{
		Number:    1,
		Slug:      "inst-01",
		Name:      "Inst 1",
		GameID:    domain.GameMinecraft,
		State:     domain.StateRunning,
		MCVersion: "1.21.1",
		Loader:    domain.LoaderNeoForge,
	})
	_ = deps.ValheimInstances.SaveInstance(domain.Instance{
		Number: 1,
		Slug:   "valheim-01",
		Name:   "Valheim 1",
		GameID: domain.GameValheim,
		State:  domain.StateRunning,
		Source: domain.SourceVanilla,
	})

	// Test MCInstances closures: WithConfigsReader, WithModsReader, WithGlobalConfigsReader
	mcCfgs, err := deps.MCInstances.ListConfigs(ctx, 1)
	if err != nil || len(mcCfgs) == 0 {
		t.Errorf("MCInstances.ListConfigs failed: %v, %v", err, mcCfgs)
	}
	mcMods, err := deps.MCInstances.GetInstalledMods(ctx, 1)
	if err != nil || len(mcMods) != 2 {
		t.Errorf("MCInstances.InstalledMods failed: %v, %v", err, mcMods)
	}
	mcGlobalCfgs, err := deps.MCInstances.ListGlobalConfigs(ctx)
	if err != nil || len(mcGlobalCfgs) == 0 {
		t.Errorf("MCInstances.ListGlobalConfigs failed: %v, %v", err, mcGlobalCfgs)
	}

	// Test ValheimInstances closures: WithConfigsReader, WithModsReader, WithGlobalConfigsReader, WithServerRefResolver
	vhCfgs, err := deps.ValheimInstances.ListConfigs(ctx, 1)
	if err != nil || len(vhCfgs) == 0 {
		t.Errorf("ValheimInstances.ListConfigs failed: %v, %v", err, vhCfgs)
	}
	vhMods, err := deps.ValheimInstances.GetInstalledMods(ctx, 1)
	if err != nil || len(vhMods) != 2 {
		t.Errorf("ValheimInstances.InstalledMods failed: %v, %v", err, vhMods)
	}
	vhGlobalCfgs, err := deps.ValheimInstances.ListGlobalConfigs(ctx)
	if err != nil || len(vhGlobalCfgs) == 0 {
		t.Errorf("ValheimInstances.ListGlobalConfigs failed: %v, %v", err, vhGlobalCfgs)
	}
	_ = deps.ValheimInstances.InstanceStats(ctx, []domain.Instance{{Number: 1, Slug: "valheim-01", GameID: domain.GameValheim, State: domain.StateRunning}})
}

func TestWiring_ApplySyncLoops_RestartFailures(t *testing.T) {
	origPoll := syncPollInterval
	origTimeout := syncTimeout
	defer func() {
		syncPollInterval = origPoll
		syncTimeout = origTimeout
	}()
	syncPollInterval = 5 * time.Millisecond
	syncTimeout = 25 * time.Millisecond

	// Fake K8s with failing deployments
	cs := fakek8s.NewSimpleClientset(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "crash-cm", Namespace: "valheim"},
			Data:       map[string]string{"sync": "true"},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-crash-cm", Namespace: "minecraft-modded"},
			Data:       map[string]string{"sync": "true"},
		},
	)
	k8sClient := k8s.NewWithClientset(cs, "valheim", "valheim")
	mcK8sClient := k8s.NewWithClientset(cs, "minecraft-modded", "minecraft-modded")

	// Missing deployment causes Patch to fail!
	d := Deps{
		K8s:            k8sClient,
		MCK8s:          mcK8sClient,
		ValheimRuntime: k8sruntime.New(k8sClient),
		MCRuntime:      k8sruntime.New(mcK8sClient),
		ValheimRef:     ports.ServerRef{Name: "nonexistent-valheim", Scope: "valheim"},
	}

	// Test applyAfterSync with failing restart
	fn1 := makeApplyAfterSync(d)
	fn1("crash-cm", "sync", func(s string) bool { return s == "true" })

	// Test applyMinecraftAfterSync with failing restart
	cfg := &config.Config{MinecraftDeployment: "nonexistent-mc", MinecraftNamespace: "minecraft-modded"}
	fn2 := makeApplyMinecraftAfterSync(cfg, d)
	fn2("mc-crash-cm", "nonexistent-mc", "sync", func(s string) bool { return s == "true" })

	// Test applyValheimAfterSync with failing restart
	cfgVh := &config.Config{ValheimDeployment: "nonexistent-valheim", ValheimNamespace: "valheim"}
	fn3 := makeApplyValheimAfterSync(cfgVh, d)
	fn3("crash-cm", "nonexistent-valheim", "sync", func(s string) bool { return s == "true" })

	time.Sleep(40 * time.Millisecond)
}

func TestWiring_Schedulers_PrunerCallbacks(t *testing.T) {
	tempDir := t.TempDir()
	st, err := store.Open(filepath.Join(tempDir, "prune.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cfg := &config.Config{
		MinecraftNamespace: "minecraft-modded",
		ValheimNamespace:   "valheim",
		BackupsDir:         tempDir,
	}

	cs := fakek8s.NewSimpleClientset()
	k8sClient := k8s.NewWithClientset(cs, "valheim", "valheim")
	mcK8sClient := k8s.NewWithClientset(cs, "minecraft-modded", "minecraft-modded")

	mcInstMgr := instances.NewInstanceManager(
		store.NewInstanceRepo(st), nil, nil, 10, 2, 1, "", "", nil, "minecraft-modded",
	)
	vhInstMgr := instances.NewInstanceManager(
		store.NewValheimInstanceRepo(st), nil, nil, 10, 2, 1, "", "", nil, "valheim",
	)

	_ = mcInstMgr.SaveInstance(domain.Instance{
		Number: 1,
		Slug:   "mc-01",
		State:  domain.StateRunning,
		GameID: domain.GameMinecraft,
	})
	_ = vhInstMgr.SaveInstance(domain.Instance{
		Number: 1,
		Slug:   "vh-01",
		State:  domain.StateRunning,
		GameID: domain.GameValheim,
	})

	d := Deps{
		Store:            st,
		K8s:              k8sClient,
		MCK8s:            mcK8sClient,
		MCInstances:      mcInstMgr,
		ValheimInstances: vhInstMgr,
	}

	ctx := context.Background()

	// Tests scheduler wiring with BackupsDir and pruner attached
	s1 := startMinecraftScheduler(ctx, cfg, d)
	s2 := startValheimScheduler(ctx, cfg, d)

	// Run backups daily pass which calls command executor and pruner closures!
	if s1 != nil {
		s1.RunDaily(ctx)
	}
	if s2 != nil {
		s2.RunDaily(ctx)
	}
}

func TestWiring_BuildServer_LegacyValheimFailure(t *testing.T) {
	tempDir := t.TempDir()
	st, err := store.Open(filepath.Join(tempDir, "adopt_fail.db"))
	if err != nil {
		t.Fatal(err)
	}

	valheimRepo := store.NewValheimInstanceRepo(st)
	cs := fakek8s.NewSimpleClientset()
	k8sClient := k8s.NewWithClientset(cs, "valheim", "valheim")

	d := Deps{
		Store: st,
		ValheimInstances: instances.NewInstanceManager(
			valheimRepo, nil, nil, 10, 2, 1, "", "", nil, "valheim",
		),
		ValheimRuntime: k8sruntime.New(k8sClient),
		ValheimRef:     ports.ServerRef{Name: "valheim"},
	}

	cfg := &config.Config{
		ValheimDeployment: "valheim",
		ValheimNamespace:  "valheim",
	}

	// Close store so repo.Get(1) fails with database closed error
	st.Close()

	app := BuildServer(context.Background(), cfg, d)
	if app == nil {
		t.Fatal("expected server app")
	}
}

func TestWiring_ValheimVersionResolver_Branches(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "vh_ver.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	tsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "notfound") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if strings.Contains(r.URL.Path, "servererr") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"latest": map[string]any{
				"version_number": "1.2.3",
			},
		})
	}))
	defer tsSrv.Close()

	cfg := &config.Config{
		DBPath:           dbPath,
		ThunderstoreAPI:  tsSrv.URL,
		ValheimNamespace: "valheim",
		LocalStateDir:    filepath.Join(tempDir, "state"),
		ComposeDir:       filepath.Join(tempDir, "compose"),
	}

	deps, err := Build(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Seed modded instance
	_ = deps.ValheimInstances.SaveInstance(domain.Instance{
		Number: 1,
		Slug:   "vh-01",
		GameID: domain.GameValheim,
		Source: domain.SourceModlist,
	})

	ctx := context.Background()

	// 1. Not a full name (no hyphen)
	_, err = deps.ValheimInstances.InstallMod(ctx, 1, "nohyphenname")
	if err == nil {
		t.Error("expected error for name without hyphen")
	}

	// 2. Not found
	_, err = deps.ValheimInstances.InstallMod(ctx, 1, "author-notfound")
	if err == nil {
		t.Error("expected error for notfound package")
	}

	// 3. Cache hit
	deps.TS.Preload([]thunderstore.SearchResult{{Owner: "author", Name: "cached", Version: "2.0.0"}}, time.Now())
	_, err = deps.ValheimInstances.InstallMod(ctx, 1, "author-cached")
	if err != nil {
		t.Errorf("unexpected error for cached package: %v", err)
	}
}
