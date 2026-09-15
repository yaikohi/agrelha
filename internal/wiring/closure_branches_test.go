package wiring

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakek8s "k8s.io/client-go/kubernetes/fake"

	"agrelha/internal/app/instances"
	"agrelha/internal/app/modpack"
	"agrelha/internal/domain"
	"agrelha/internal/infra/content/modpackindex"
	"agrelha/internal/infra/content/modrinth"
	"agrelha/internal/infra/content/thunderstore"
	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/store"
	"agrelha/internal/platform/config"
	backupshttp "agrelha/internal/web/handlers/backups"
	minecrafthttp "agrelha/internal/web/handlers/minecraft"
	valheimhttp "agrelha/internal/web/handlers/valheim"
)

type dummyRenderer struct{}

func (dummyRenderer) Render(domain.Instance, string) (map[string][]byte, error) {
	return map[string][]byte{}, nil
}

func TestWiring_BuildHandlers_RemainingBranches(t *testing.T) {
	ctx := context.Background()

	// 1. buildContentHandler: ConfigData when d.K8s == nil
	contentH := buildContentHandler(&config.Config{}, Deps{}, nil)
	cApp := fiber.New()
	contentH.RegisterProtected(cApp)
	cReq := httptest.NewRequest(fiber.MethodGet, "/configs", nil)
	_, _ = cApp.Test(cReq)

	// 2. buildMinecraftHandler: error branches in WithConfigsReader, WithModsReader, and WithGlobalConfigsReader
	cs := fakek8s.NewSimpleClientset(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "minecraft-modded-configs", Namespace: "minecraft-modded"},
			Data:       map[string]string{"server.properties": "online-mode=true"},
		},
	)
	mcK8sClient := k8s.NewWithClientset(cs, "minecraft-modded", "minecraft-modded")
	tempDir := t.TempDir()
	st, err := store.Open(filepath.Join(tempDir, "mc_branches.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mcRepo := store.NewInstanceRepo(st)
	mcMgr := instances.NewInstanceManager(mcRepo, nil, nil, 10, 2, 1, "", "", &dummyRenderer{}, "minecraft-modded")
	d := Deps{
		Store:       st,
		MCInstances: mcMgr,
		MCK8s:       mcK8sClient,
	}

	// Trigger buildMinecraftHandler to attach the readers
	_ = buildMinecraftHandler(&config.Config{BackupsDir: tempDir}, d, nil)

	// Call readers with non-existent instance 999
	if _, err := mcMgr.ListConfigs(ctx, 999); err == nil {
		t.Error("expected error for non-existent instance in ListConfigs, got nil")
	}
	if _, err := mcMgr.GetInstalledMods(ctx, 999); err == nil {
		t.Error("expected error for non-existent instance in GetInstalledMods, got nil")
	}
	// Call global configs reader
	if gcfgs, err := mcMgr.ListGlobalConfigs(ctx); err != nil || len(gcfgs) == 0 {
		t.Errorf("ListGlobalConfigs failed: %v, %v", gcfgs, err)
	}

	// 3. buildMinecraftHandler: searchMods error
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer failSrv.Close()

	failMR := modrinth.New(failSrv.URL)
	dFailMR := Deps{MR: failMR}
	mcHFail := buildMinecraftHandler(&config.Config{}, dFailMR, nil)
	app := fiber.New()
	mcHFail.RegisterProtected(app)
	req := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/1/mods/search", strings.NewReader("q=err"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, _ = app.Test(req)

	// 4. buildWizardHandler: searchModpacks error & resolvePackRef & verifyPackLoader & searchMods error
	mpiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/modpacks") && r.URL.Query().Get("search") == "err":
			http.Error(w, "mpi error", http.StatusInternalServerError)
		case strings.Contains(r.URL.Path, "/modpack/999"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"links": map[string]string{"curseforge": "https://curseforge.com/modpack/detail"},
				},
			})
		case strings.Contains(r.URL.Path, "/modpack/888"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"url": "https://curseforge.com/modpack/direct",
				},
			})
		case strings.Contains(r.URL.Path, "/modpack/777"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"url": "https://other.com/pack",
				},
			})
		case strings.Contains(r.URL.Path, "/modpack/100/mods"):
			// Fabric-only mods to correct loader
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"name": "fabric-api",
						"modrinth_info": []map[string]any{
							{"loaders": []string{"fabric"}},
						},
					},
				},
			})
		case strings.Contains(r.URL.Path, "/modpack/101/mods"):
			// Error loading mods
			http.Error(w, "error", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer mpiSrv.Close()

	mpiClient := modpackindex.New(mpiSrv.URL)
	dWizard := Deps{
		MPI:         mpiClient,
		MR:          failMR,
		MCInstances: mcMgr,
	}
	wizH := buildWizardHandler(dWizard)
	wizApp := fiber.New()
	wizH.Register(wizApp)

	// SearchModpacks error route
	reqWiz := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/modpacks/search", strings.NewReader("packQuery=err"))
	reqWiz.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, _ = wizApp.Test(reqWiz)

	// SearchMods error route
	reqWizMod := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/mods/search", strings.NewReader("modQuery=err&version=1.21.1"))
	reqWizMod.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, _ = wizApp.Test(reqWizMod)

	// SearchMods success route
	mrOkSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"hits": []map[string]any{
				{"slug": "jei", "title": "Just Enough Items", "description": "Items"},
			},
			"total_hits": 1,
		})
	}))
	defer mrOkSrv.Close()
	dWizard.MR = modrinth.New(mrOkSrv.URL)
	wizHOk := buildWizardHandler(dWizard)
	wizAppOk := fiber.New()
	wizHOk.Register(wizAppOk)
	reqWizOk := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/mods/search", strings.NewReader("modQuery=jei&version=1.21.1"))
	reqWizOk.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, _ = wizAppOk.Test(reqWizOk)

	// Wizard create calling resolvePackRef and verifyPackLoader
	for _, pack := range []struct {
		id   string
		ref  string
		load string
	}{
		{"999", "https://modpackindex.com/modpack/999/detail", "neoforge"},
		{"888", "https://modpackindex.com/modpack/888/view", "neoforge"},
		{"777", "https://modpackindex.com/modpack/777/view", "neoforge"},
		{"100", "https://curseforge.com/modpack/direct", "neoforge"},
		{"100", "https://curseforge.com/modpack/direct", "fabric"},
		{"101", "https://other.com/pack", "fabric"},
	} {
		f := url.Values{
			"name":      {"Pack Test"},
			"source":    {"modpack"},
			"pack_id":   {pack.id},
			"pack_name": {"Pack " + pack.id},
			"pack_ref":  {pack.ref},
			"loader":    {pack.load},
		}
		reqCreate := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/create", strings.NewReader(f.Encode()))
		reqCreate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_, _ = wizApp.Test(reqCreate)
	}

	// 5. buildDashboardHandler: BackupInfo, instStats, and vhInstStats
	vhRepo := store.NewValheimInstanceRepo(st)
	vhMgr := instances.NewInstanceManager(vhRepo, nil, nil, 10, 2, 1, "", "", nil, "valheim")
	_ = mcRepo.Upsert(domain.Instance{
		Number: 1,
		Slug:   "mc-stat",
		Name:   "MC Stat",
		State:  domain.StateRunning,
	})
	_ = vhRepo.Upsert(domain.Instance{
		Number: 1,
		Slug:   "vh-stat",
		Name:   "VH Stat",
		State:  domain.StateRunning,
	})

	mcH := minecrafthttp.New(minecrafthttp.Config{
		MCInstances: mcMgr,
	})
	vhH := valheimhttp.New(valheimhttp.Config{
		ValheimInstances: vhMgr,
	})
	bkDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(bkDir, "mc-stat-01-daily.tar.gz"), []byte("data"), 0644)
	bkH := backupshttp.New(backupshttp.Config{
		BackupsDir: bkDir,
	})

	dDash := Deps{
		MCInstances:      mcMgr,
		ValheimInstances: vhMgr,
	}
	dashH := buildDashboardHandler(&config.Config{}, dDash, contentH, mcH, bkH, vhH)
	dashApp := fiber.New()
	dashH.Register(dashApp)
	reqDash := httptest.NewRequest(fiber.MethodGet, "/", nil)
	_, _ = dashApp.Test(reqDash)
	_ = dashH.TileSignals(ctx)
}

func TestWiring_WiringRemainingBranches(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "wiring_more.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// 1. RCON Mock Server
	rconAddr, stopRcon := mockRconServer(t, "rconpass")
	defer stopRcon()

	// 2. Mock Thunderstore Server
	tsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "BepInExPack_Valheim") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"latest": map[string]any{"version_number": "5.4.2202"},
			})
			return
		}
		if strings.Contains(r.URL.Path, "author-testmod") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"latest": map[string]any{"version_number": "1.0.0"},
			})
			return
		}
		if strings.Contains(r.URL.Path, "author-err") {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		if strings.Contains(r.URL.Path, "author-fresh") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"latest": map[string]any{"version_number": "2.0.0"},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer tsSrv.Close()

	// 3. Mock Modrinth Server
	mrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/project/") {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "p1", "slug": "slug1"})
			return
		}
		http.NotFound(w, r)
	}))
	defer mrSrv.Close()

	cfg := &config.Config{
		DBPath:                dbPath,
		ThunderstoreAPI:       tsSrv.URL,
		ModrinthAPI:           mrSrv.URL,
		MinecraftRconAddr:     rconAddr,
		MinecraftRconPassword: "rconpass",
		MinecraftDeployment:   "minecraft-modded",
		MinecraftNamespace:    "minecraft-modded",
		ValheimDeployment:     "valheim",
		ValheimNamespace:      "valheim",
		LocalStateDir:         filepath.Join(tempDir, "state"),
		ComposeDir:            filepath.Join(tempDir, "compose"),
	}

	cs := fakek8s.NewSimpleClientset(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-01-mods", Namespace: "minecraft-modded"},
			Data: map[string]string{
				"mods.txt": "# comment\njei\n\nwaystones?\n",
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-01-configs", Namespace: "minecraft-modded"},
			Data:       map[string]string{"config.json": "{}"},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim-mods", Namespace: "valheim"},
			Data:       map[string]string{"mods.txt": "author-testmod\n"},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim-mod-configs", Namespace: "valheim"},
			Data:       map[string]string{"valheim.cfg": "key=val"},
		},
	)
	origNewK8s := newK8sClient
	newK8sClient = func(ns, dep string) (*k8s.Client, error) {
		return k8s.NewWithClientset(cs, ns, dep), nil
	}
	defer func() { newK8sClient = origNewK8s }()

	deps, err := Build(ctx, cfg)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Seed an instance with pack defined for ActiveInstance
	mcInst := domain.Instance{
		Number:    1,
		Slug:      "mc-01",
		Name:      "Minecraft 01",
		GameID:    domain.GameMinecraft,
		State:     domain.StateRunning,
		Loader:    domain.LoaderNeoForge,
		MCVersion: "1.21.1",
		Source:    domain.SourceModpack,
		Pack: &domain.Pack{
			Provider: domain.ProviderCurseForge,
			Ref:      "1",
			Name:     "All The Mods",
		},
		LBIP: rconAddr,
	}
	_ = deps.MCInstances.SaveInstance(mcInst)

	// Seed valheim instance
	vhInst := domain.Instance{
		Number: 1,
		Slug:   "vh-01",
		Name:   "Valheim 01",
		GameID: domain.GameValheim,
		State:  domain.StateRunning,
		Source: domain.SourceModlist,
	}
	_ = deps.ValheimInstances.SaveInstance(vhInst)

	// Exercise PreStop and PreDelete hooks and Telemetry
	_ = deps.MCInstances.StopInstance(ctx, 1)
	_ = deps.MCInstances.DeleteInstance(ctx, 1)
	_ = deps.MCInstances.SaveInstance(mcInst) // re-save
	stats := deps.MCInstances.InstanceStats(ctx, []domain.Instance{mcInst})
	t.Logf("instance stats: %+v", stats)

	// Exercise MinecraftGame Telemetry & ExportClientBundle
	mcTelem, err := deps.MinecraftGame.Telemetry(ctx)
	t.Logf("MC Telemetry: %+v, err: %v", mcTelem, err)

	bundle, err := deps.MinecraftGame.ExportClientBundle(ctx, mcInst)
	t.Logf("MC ExportClientBundle: %+v, err: %v", bundle, err)

	// Exercise ValheimGame Telemetry & ExportClientBundle
	vhTelem, err := deps.ValheimGame.Telemetry(ctx)
	t.Logf("VH Telemetry: %+v, err: %v", vhTelem, err)

	vhBundle, err := deps.ValheimGame.ExportClientBundle(ctx, vhInst)
	t.Logf("VH ExportClientBundle: %+v, err: %v", vhBundle, err)

	// Valheim bundle source with cache hit and with no-hyphen name
	deps.TS.Preload([]thunderstore.SearchResult{{Owner: "author", Name: "testmod", Version: "1.0.0"}}, time.Now())
	_ = cs.CoreV1().ConfigMaps("valheim").Delete(ctx, "valheim-mods", metav1.DeleteOptions{})
	_, _ = cs.CoreV1().ConfigMaps("valheim").Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "valheim-mods", Namespace: "valheim"},
		Data:       map[string]string{"mods.txt": "author-testmod\nnohyphen\n"},
	}, metav1.CreateOptions{})
	vhBundle2, err := deps.ValheimGame.ExportClientBundle(ctx, domain.Instance{Number: 0})
	t.Logf("VH ExportClientBundle 2: %+v, err: %v", vhBundle2, err)

	// Valheim WithConfigsReader and WithModsReader error branches on non-existent instance
	if _, err := deps.ValheimInstances.ListConfigs(ctx, 999); err == nil {
		t.Error("expected error for non-existent instance in ListConfigs")
	}
	if _, err := deps.ValheimInstances.GetInstalledMods(ctx, 999); err == nil {
		t.Error("expected error for non-existent instance in GetInstalledMods")
	}

	// MC WithConfigsReader and WithModsReader error branches on non-existent instance
	if _, err := deps.MCInstances.ListConfigs(ctx, 999); err == nil {
		t.Error("expected error for non-existent instance in MC ListConfigs")
	}
	if _, err := deps.MCInstances.GetInstalledMods(ctx, 999); err == nil {
		t.Error("expected error for non-existent instance in MC GetInstalledMods")
	}

	// Valheim InstallMod error and success branches
	_, _ = deps.ValheimInstances.InstallMod(ctx, 1, "author-err")
	_, _ = deps.ValheimInstances.InstallMod(ctx, 1, "author-fresh")

	// Valheim WithModsReader when CM is deleted
	_ = cs.CoreV1().ConfigMaps("valheim").Delete(ctx, "valheim-mods", metav1.DeleteOptions{})
	_, _ = deps.ValheimInstances.GetInstalledMods(ctx, 1)

	// Stop RCON and test MCAccess error in MinecraftGame.Telemetry
	stopRcon()
	_, _ = deps.MinecraftGame.Telemetry(ctx)
}

func TestWiring_SyncLoops_ConfigMapDataErrors(t *testing.T) {
	origPoll := syncPollInterval
	origTimeout := syncTimeout
	syncPollInterval = 1 * time.Millisecond
	syncTimeout = 15 * time.Millisecond
	defer func() {
		syncPollInterval = origPoll
		syncTimeout = origTimeout
	}()

	// Fake client that returns error for ConfigMapData because CM does not exist
	cs := fakek8s.NewSimpleClientset()
	k8sClient := k8s.NewWithClientset(cs, "test-ns", "test-dep")
	d := Deps{
		K8s:   k8sClient,
		MCK8s: k8sClient,
	}

	// 1. makeApplyAfterSync error
	fn1 := makeApplyAfterSync(d)
	fn1("nonexistent-cm", "key", func(v string) bool { return true })

	// 2. makeApplyMinecraftAfterSync error
	fn2 := makeApplyMinecraftAfterSync(&config.Config{}, d)
	fn2("nonexistent-mc-cm", "dep", "key", func(v string) bool { return true })

	// 3. makeApplyValheimAfterSync error
	fn3 := makeApplyValheimAfterSync(&config.Config{}, d)
	fn3("nonexistent-vh-cm", "dep", "key", func(v string) bool { return true })

	time.Sleep(30 * time.Millisecond)
}

func TestWiring_BuildDeclarativePlane_DefaultDockerDir(t *testing.T) {
	tempDir := t.TempDir()
	st, err := store.Open(filepath.Join(tempDir, "plane.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Runtime == "docker" with empty LocalStateDir
	cfg := &config.Config{
		Runtime:       "docker",
		LocalStateDir: "",
		ComposeDir:    filepath.Join(tempDir, "compose"),
	}
	ss, rec, committer := buildDeclarativePlane(cfg, st)
	t.Logf("buildDeclarativePlane docker default: ss=%v rec=%v committer=%v", ss, rec, committer)
}

func TestWiring_BuildAuth_OIDCSuccess(t *testing.T) {
	// Mock OIDC discovery server
	var oidcServer *httptest.Server
	oidcServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/.well-known/openid-configuration" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                 oidcServer.URL,
				"authorization_endpoint": oidcServer.URL + "/auth",
				"token_endpoint":         oidcServer.URL + "/token",
				"jwks_uri":               oidcServer.URL + "/jwks",
			})
			return
		}
		if r.URL.Path == "/jwks" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"keys": []any{},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer oidcServer.Close()

	tempDir := t.TempDir()
	st, err := store.Open(filepath.Join(tempDir, "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cfg := &config.Config{
		OIDCIssuer:       oidcServer.URL,
		OIDCClientID:     "client-id",
		OIDCClientSecret: "client-secret",
	}

	auth := buildAuth(context.Background(), cfg, st)
	if auth == nil {
		t.Fatal("expected non-nil auth from buildAuth")
	}
}

func TestWiring_ValheimBackfill_Errors(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	st, err := store.Open(filepath.Join(tempDir, "backfill.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Seed an instance with empty source in store
	vhRepo := store.NewValheimInstanceRepo(st)
	_ = vhRepo.Upsert(domain.Instance{
		Number: 1,
		Slug:   "vh-bf",
		Name:   "Valheim Backfill",
		GameID: domain.GameValheim,
	})

	cs := fakek8s.NewSimpleClientset()
	k8sClient := k8s.NewWithClientset(cs, "valheim", "valheim")
	mgr := instances.NewInstanceManager(vhRepo, nil, nil, 10, 2, 1, "", "", nil, "valheim")

	d := Deps{
		Store:            st,
		ValheimInstances: mgr,
		K8s:              k8sClient,
	}

	// 1. backfill runs once
	backfillValheimSource(ctx, &d)

	// 2. Now force error by closing store
	st.Close()
	backfillValheimSource(ctx, &d)
}

type mockFailingRepo struct {
	getErr    error
	upsertErr error
}

func (m *mockFailingRepo) Upsert(inst domain.Instance) error {
	return m.upsertErr
}

func (m *mockFailingRepo) Get(num int) (*domain.Instance, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return &domain.Instance{Number: num, Slug: "vh-load", Name: "VH Load"}, nil
}

func (m *mockFailingRepo) List() ([]domain.Instance, error) {
	return nil, nil
}

func (m *mockFailingRepo) UpdateState(int, domain.InstanceState) error {
	return nil
}

func (m *mockFailingRepo) Delete(int) error {
	return nil
}

func TestWiring_BackfillValheimSource_MoreErrors(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	st, err := store.Open(filepath.Join(tempDir, "backfill_err.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Direct SQL insert into valheim_instances missing source
	_, _ = st.DB().Exec("INSERT INTO valheim_instances (number, slug, name, state, memory_gib, cpu_cores, source) VALUES (1, 'vh-1', 'VH 1', 'stopped', 4, 2, '')")
	_, _ = st.DB().Exec("INSERT INTO valheim_instances (number, slug, name, state, memory_gib, cpu_cores, source) VALUES (2, 'vh-2', 'VH 2', 'stopped', 4, 2, '')")

	cs := fakek8s.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim-vh-load-02", Namespace: "valheim"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "valheim",
								Env: []corev1.EnvVar{
									{Name: "BEPINEX", Value: "false"},
								},
							},
						},
					},
				},
			},
		},
	)
	k8sClient := k8s.NewWithClientset(cs, "valheim", "valheim")

	// 1. GetInstance failure (lines 598-601)
	failRepo1 := &mockFailingRepo{getErr: errors.New("cannot load")}
	vhMgr1 := instances.NewInstanceManager(failRepo1, nil, nil, 10, 2, 1, "", "", nil, "valheim")
	d1 := &Deps{
		Store:            st,
		K8s:              k8sClient,
		ValheimInstances: vhMgr1,
	}
	backfillValheimSource(ctx, d1)

	// 2. SaveInstance failure (lines 610-613)
	failRepo2 := &mockFailingRepo{upsertErr: errors.New("cannot save")}
	vhMgr2 := instances.NewInstanceManager(failRepo2, nil, nil, 10, 2, 1, "", "", nil, "valheim")
	d2 := &Deps{
		Store:            st,
		K8s:              k8sClient,
		ValheimInstances: vhMgr2,
	}
	backfillValheimSource(ctx, d2)
}

func TestWiring_BuildThunderstore_ErrorBranches(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "ts_err.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	// Close store so LoadModIndex errors
	st.Close()

	cfg := &config.Config{
		ThunderstoreAPI: "http://127.0.0.1:0",
	}
	ts := buildThunderstore(ctx, cfg, st)
	if ts.OnRefresh != nil {
		ts.OnRefresh([]thunderstore.SearchResult{{Owner: "test", Name: "mod", Version: "1.0"}})
	}
}

func TestWiring_ValheimGame_Options_Branches(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "vh_opt.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cs := fakek8s.NewSimpleClientset(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim-vh-01-mods", Namespace: "valheim"},
			Data: map[string]string{
				"mods.txt": "denikson-BepInExPack_Valheim\nnohyphenmod\nsome-other-mod\n",
			},
		},
	)
	origNewK8s := newK8sClient
	newK8sClient = func(ns, dep string) (*k8s.Client, error) {
		return k8s.NewWithClientset(cs, ns, dep), nil
	}
	defer func() { newK8sClient = origNewK8s }()

	cfg := &config.Config{
		DBPath:            dbPath,
		ValheimDeployment: "valheim",
		ValheimNamespace:  "valheim",
	}
	deps, err := Build(ctx, cfg)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// 1. WithBepInExVersion when TS is nil
	deps.TS = nil
	_, _ = deps.ValheimGame.ExportClientBundle(ctx, domain.Instance{Number: 1, Slug: "vh", Name: "VH", Source: domain.SourceModlist})

	// 2. WithBepInExVersion when TS has cached hit
	tsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version_number": "1.0.0",
		})
	}))
	defer tsSrv.Close()

	tsClient := thunderstore.New(tsSrv.URL)
	tsClient.Preload([]thunderstore.SearchResult{
		{Owner: "denikson", Name: "BepInExPack_Valheim", Version: "5.4.2202"},
	}, time.Now())
	deps.TS = tsClient

	// 3. ExportClientBundle with cached hit, no-hyphen mod, and TS lookup
	vhInst := domain.Instance{Number: 1, Slug: "vh", Name: "VH", Source: domain.SourceModlist}
	_, _ = deps.ValheimGame.ExportClientBundle(ctx, vhInst)

	// 4. ExportClientBundle when TS is nil
	deps.TS = nil
	_, _ = deps.ValheimGame.ExportClientBundle(ctx, vhInst)

	// 5. Minecraft PlayerCount when MCAccess is nil -> returns 0, nil
	deps.MCAccess = nil
	_, _ = deps.MinecraftGame.Telemetry(ctx)
}

func TestWiring_BackfillValheimSource_Branches(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	// DB 1: has instance 99 with empty source
	dbPath1 := filepath.Join(tempDir, "vh_bf1.db")
	st1, err := store.Open(dbPath1)
	if err != nil {
		t.Fatal(err)
	}
	defer st1.Close()

	_, err = st1.DB().Exec(`INSERT INTO valheim_instances (number, name, slug, source)
		VALUES (99, 'unloaded', 'unloaded', '')`)
	if err != nil {
		t.Fatal(err)
	}

	// DB 2: empty store for InstanceManager, so GetInstance(99) returns not found!
	dbPath2 := filepath.Join(tempDir, "vh_bf2.db")
	st2, err := store.Open(dbPath2)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()

	mgr2 := instances.NewInstanceManager(store.NewValheimInstanceRepo(st2), nil, nil, 10, 2, 1, "", "", &dummyRenderer{}, "valheim")

	cs := fakek8s.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim-unloaded-99", Namespace: "valheim"},
		},
	)
	k8sClient := k8s.NewWithClientset(cs, "valheim", "valheim")

	d := &Deps{
		Store:            st1,
		ValheimInstances: mgr2,
		K8s:              k8sClient,
	}

	// 1. Cannot load instance (num 99 not in mgr2) -> triggers lines 600-602
	backfillValheimSource(ctx, d)

	// 2. Cannot save instance -> insert 99 into mgr3 with closed DB so SaveInstance fails
	dbPath3 := filepath.Join(tempDir, "vh_bf3.db")
	st3, err := store.Open(dbPath3)
	if err != nil {
		t.Fatal(err)
	}
	vhRepo3 := store.NewValheimInstanceRepo(st3)
	if err := vhRepo3.Upsert(domain.Instance{GameID: domain.GameValheim, Number: 99, Name: "savetest", Slug: "savetest", State: domain.StateRunning}); err != nil {
		t.Fatal(err)
	}
	_ = st3.Close()

	mgr3 := instances.NewInstanceManager(vhRepo3, nil, nil, 10, 2, 1, "", "", &dummyRenderer{}, "valheim")
	d3 := &Deps{
		Store:            st1,
		ValheimInstances: mgr3,
		K8s:              k8sClient,
	}
	// Triggers lines 612-614
	backfillValheimSource(ctx, d3)
}

func TestWiring_MinecraftGame_Options_Branches(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "mc_opt.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cs := fakek8s.NewSimpleClientset(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-mc-1-01-mods", Namespace: "mc"},
			Data: map[string]string{
				"mods.txt": "jei?\n# comment\nwaystones\n",
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-mc-1-01-configs", Namespace: "mc"},
			Data:       map[string]string{"server.properties": "online-mode=true"},
		},
	)
	origNewK8s := newK8sClient
	newK8sClient = func(ns, dep string) (*k8s.Client, error) {
		return k8s.NewWithClientset(cs, ns, dep), nil
	}
	defer func() { newK8sClient = origNewK8s }()

	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "mr error", http.StatusInternalServerError)
	}))
	defer failSrv.Close()

	cfg := &config.Config{
		DBPath:              dbPath,
		MinecraftDeployment: "mc",
		MinecraftNamespace:  "mc",
		ModrinthAPI:         failSrv.URL,
		LocalStateDir:       filepath.Join(tempDir, "state"),
		ComposeDir:          filepath.Join(tempDir, "compose"),
	}
	deps, err := Build(ctx, cfg)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	telem, _ := deps.MinecraftGame.Telemetry(ctx)
	t.Logf("MC telem with no MCAccess: %+v", telem)

	oldMrpack := buildMrpack
	buildMrpack = func(ctx context.Context, mr modpack.ModrinthProvider, packName, mcVersion, loaderType, loaderVersion string, slugs []string, configs map[string]string) ([]byte, error) {
		return nil, errors.New("mrpack build failed")
	}
	defer func() { buildMrpack = oldMrpack }()

	mcInst := domain.Instance{Number: 1, Slug: "mc-1", Name: "MC 1"}
	_, err = deps.MinecraftGame.ExportClientBundle(ctx, mcInst)
	if err == nil {
		t.Error("expected error from ExportClientBundle with failing buildMrpack")
	}
}
