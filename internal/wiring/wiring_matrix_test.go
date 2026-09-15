package wiring

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakek8s "k8s.io/client-go/kubernetes/fake"

	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"agrelha/internal/infra/auth/local"
	"agrelha/internal/infra/content/modpackindex"
	"agrelha/internal/infra/content/thunderstore"
	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/store"
	"agrelha/internal/platform/config"
	"agrelha/internal/ports"
)

// mockRconServer creates an in-process Source RCON server for tests.
func mockRconServer(t *testing.T, expectedPassword string) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	stop := make(chan struct{})

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				for {
					var length int32
					if err := binary.Read(c, binary.LittleEndian, &length); err != nil {
						return
					}
					if length < 10 || length > 4096 {
						return
					}
					data := make([]byte, length)
					if _, err := io.ReadFull(c, data); err != nil {
						return
					}
					buf := bytes.NewReader(data)
					var id, typ int32
					_ = binary.Read(buf, binary.LittleEndian, &id)
					_ = binary.Read(buf, binary.LittleEndian, &typ)
					body := string(data[8 : len(data)-2])

					switch typ {
					case 3: // auth
						if body == expectedPassword {
							sendMockPacket(c, id, 2, "")
						} else {
							sendMockPacket(c, -1, 2, "")
						}
					case 2: // command
						res := "Unknown command"
						if strings.HasPrefix(body, "/say") {
							res = "[Server] " + strings.TrimPrefix(body, "/say ")
						} else if strings.HasPrefix(body, "/list") {
							res = "There are 2 of a max of 20 players online: Alice, Bob"
						} else {
							res = "OK: " + body
						}
						sendMockPacket(c, id, 0, res)
					}
				}
			}(conn)
		}
	}()

	var once sync.Once
	return ln.Addr().String(), func() {
		once.Do(func() {
			close(stop)
			_ = ln.Close()
		})
	}
}

func sendMockPacket(w io.Writer, id, typ int32, body string) {
	b := []byte(body)
	length := int32(4 + 4 + len(b) + 2)
	_ = binary.Write(w, binary.LittleEndian, length)
	_ = binary.Write(w, binary.LittleEndian, id)
	_ = binary.Write(w, binary.LittleEndian, typ)
	_, _ = w.Write(b)
	_, _ = w.Write([]byte{0, 0})
}

func TestWiring_Build_FullK8sMatrix(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "matrix.db")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Seed user for local auth
	hash, err := local.HashPassword("secretpass")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUser(t.Context(), ports.User{Username: "admin", Email: "admin@test.local"}, hash); err != nil {
		t.Fatal(err)
	}

	// Seed Thunderstore mod cache
	_ = st.SaveModIndex([]store.ModIndexRow{
		{FullName: "denikson-BepInExPack_Valheim", Version: "5.4.2202"},
		{FullName: "valheim-mod-1", Version: "1.0.0"},
	}, time.Now())

	// Seed Valheim instance missing source
	valheimRepo := store.NewValheimInstanceRepo(st)
	_ = valheimRepo.Upsert(domain.Instance{
		Number: 1,
		Slug:   "valheim-01",
		Name:   "Valheim 1",
		GameID: domain.GameValheim,
		Source: "", // missing source to trigger backfill!
	})

	// Seed Minecraft instance
	mcRepo := store.NewInstanceRepo(st)
	_ = mcRepo.Upsert(domain.Instance{
		Number:    1,
		Slug:      "mc-matrix-01",
		Name:      "Matrix Craft",
		GameID:    domain.GameMinecraft,
		MCVersion: "1.21.1",
		Loader:    domain.LoaderNeoForge,
		Source:    domain.SourceModlist,
		LBIP:      "127.0.0.1",
	})

	// Mock RCON
	rconAddr, stopRcon := mockRconServer(t, "matrixpass")
	defer stopRcon()

	// Mock Modrinth Server
	mrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/v2/search") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hits": []map[string]any{
					{"slug": "jei", "title": "Just Enough Items", "description": "Item viewer", "icon_url": "http://img/jei.png"},
				},
				"total_hits": 1,
			})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v2/project/") && strings.HasSuffix(r.URL.Path, "/version") {
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"version_number": "1.0.0",
					"game_versions":  []string{"1.21.1"},
					"loaders":        []string{"neoforge"},
					"files": []map[string]any{
						{"primary": true, "url": "http://example.com/mod.jar", "filename": "mod.jar"},
					},
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer mrSrv.Close()

	// Fake K8s clientset
	one := int32(1)
	cs := fakek8s.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim", Namespace: "valheim"},
			Spec: appsv1.DeploymentSpec{
				Replicas: &one,
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
			Status: appsv1.DeploymentStatus{
				Replicas:      1,
				ReadyReplicas: 1,
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim-valheim-01-01", Namespace: "valheim"},
			Spec: appsv1.DeploymentSpec{
				Replicas: &one,
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
			Status: appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim-mods", Namespace: "valheim"},
			Data: map[string]string{
				"mods.txt": "denikson-BepInExPack_Valheim\n",
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim-mod-configs", Namespace: "valheim"},
			Data: map[string]string{
				"bepinex.cfg": "[Logging]\nEnabled=true\n",
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "minecraft-modded", Namespace: "minecraft-modded"},
			Spec:       appsv1.DeploymentSpec{Replicas: &one},
			Status:     appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-matrix-01-mods", Namespace: "minecraft-modded"},
			Data: map[string]string{
				"mods.txt": "jei\nappleskin?\n# test comment\n",
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-matrix-01-configs", Namespace: "minecraft-modded"},
			Data: map[string]string{
				"server.properties": "server-port=25565\n",
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "minecraft-modded-configs", Namespace: "minecraft-modded"},
			Data: map[string]string{
				"global.json": `{"test":true}`,
			},
		},
	)

	origK8sClient := newK8sClient
	defer func() { newK8sClient = origK8sClient }()
	newK8sClient = func(ns, dep string) (*k8s.Client, error) {
		return k8s.NewWithClientset(cs, ns, dep), nil
	}

	cfg := &config.Config{
		DBPath:                dbPath,
		MinecraftRconPassword: "matrixpass",
		MinecraftRconAddr:     rconAddr,
		ValheimDeployment:     "valheim",
		ValheimNamespace:      "valheim",
		MinecraftDeployment:   "minecraft-modded",
		MinecraftNamespace:    "minecraft-modded",
		ModrinthAPI:           mrSrv.URL,
		BackupsDir:            tempDir,
		ModsPath:              filepath.Join(tempDir, "mods.txt"),
		AdminsPath:            filepath.Join(tempDir, "admins.txt"),
		MinecraftModsPath:     filepath.Join(tempDir, "mcmods.txt"),
		MinecraftAccessPath:   filepath.Join(tempDir, "mcaccess.txt"),
		GameNodeSelector:      "game-node=game-01",
		MCTotalBudgetGiB:      24,
		MCMaxInstances:        5,
		MCMaxRunning:          2,
		ValheimTotalBudgetGiB: 16,
		ValheimMaxInstances:   4,
		ValheimMaxRunning:     2,
	}

	ctx := t.Context()
	deps, err := Build(ctx, cfg)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Verify backfill executed
	backfilledInst, err := deps.ValheimInstances.GetInstance(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if backfilledInst.Source != domain.SourceVanilla {
		t.Errorf("expected backfilled source Vanilla, got %v", backfilledInst.Source)
	}

	// Trigger Thunderstore onRefresh callback
	if deps.TS != nil && deps.TS.OnRefresh != nil {
		deps.TS.OnRefresh([]thunderstore.SearchResult{
			{Owner: "test", Name: "pkg", Version: "1.0.0"},
		})
	}

	// Test Game Port methods that exercise wiring closures:
	// 1. ValheimGame ExportClientBundle
	vhBundle, err := deps.ValheimGame.ExportClientBundle(ctx, *backfilledInst)
	if err != nil {
		t.Errorf("ValheimGame.ExportClientBundle error: %v", err)
	} else if vhBundle.Filename == "" {
		t.Errorf("expected valheim bundle filename")
	}

	// 2. ValheimGame Telemetry
	_, _ = deps.ValheimGame.Telemetry(ctx)

	// 3. MinecraftGame Telemetry
	_, _ = deps.MinecraftGame.Telemetry(ctx)

	// 4. MinecraftGame ExportClientBundle
	mcInst, _ := deps.MCInstances.GetInstance(ctx, 1)
	if mcInst != nil {
		_, _ = deps.MinecraftGame.ExportClientBundle(ctx, *mcInst)
	}

	// 5. Test MCInstances RCON hooks (execute, telemetry, stop, delete)
	if deps.MCInstances != nil {
		res, err := deps.MCInstances.ExecuteCommand(ctx, 1, "say testing rcon")
		if err != nil {
			t.Logf("ExecuteCommand note: %v", err)
		} else if !strings.Contains(res, "Server") {
			t.Errorf("unexpected execute response: %s", res)
		}
		// Telemetry provider callback
		_ = deps.MCInstances.InstanceStats(ctx, []domain.Instance{{Number: 1, State: domain.StateRunning}})

		// Stop and Delete hooks
		_ = deps.MCInstances.StopInstance(ctx, 1)
		_ = deps.MCInstances.DeleteInstance(ctx, 1)
	}

	// 6. Test bundle builders with defaults
	_, _ = deps.ValheimGame.ExportClientBundle(ctx, domain.Instance{Number: 0})
	_, _ = deps.MinecraftGame.ExportClientBundle(ctx, domain.Instance{Number: 1, Slug: "mc-matrix-01", Loader: "", MCVersion: ""})

	// Build Server
	app := BuildServer(ctx, cfg, deps)

	// Call endpoints to exercise handlers and callbacks
	routes := []string{
		"/",
		"/valheim",
		"/valheim/1",
		"/minecraft",
		"/minecraft/1",
		"/minecraft/create",
		"/api/minecraft/wizard/releases",
	}
	for _, r := range routes {
		req := httptest.NewRequest(fiber.MethodGet, r, nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Errorf("GET %s failed: %v", r, err)
		} else if resp.StatusCode >= 500 {
			t.Errorf("GET %s returned 5xx: %d", r, resp.StatusCode)
		}
	}
}

func TestWiring_Build_FallbacksAndErrors(t *testing.T) {
	// 1. Invalid DB
	_, err := Build(context.Background(), &config.Config{DBPath: "/dev/null/impossible/db.sqlite"})
	if err == nil {
		t.Error("expected error for invalid DB path")
	}

	// 2. K8s client error fallback
	origK8sClient := newK8sClient
	defer func() { newK8sClient = origK8sClient }()
	newK8sClient = func(ns, dep string) (*k8s.Client, error) {
		return nil, fmt.Errorf("fake connection refused")
	}

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "fallback.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cfg := &config.Config{
		DBPath:            dbPath,
		Runtime:           "docker",
		LocalStateDir:     filepath.Join(tempDir, "local_state"),
		ComposeDir:        filepath.Join(tempDir, "compose"),
		MinecraftRconAddr: "127.0.0.1:0",
	}

	deps, err := Build(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if deps.K8s != nil || deps.MCK8s != nil {
		t.Errorf("expected K8s and MCK8s to be nil on client error")
	}

	// ValheimGame bundle with nil K8s
	bBundle, err := deps.ValheimGame.ExportClientBundle(context.Background(), domain.Instance{Number: 1})
	if err != nil || bBundle.Filename == "" {
		t.Errorf("expected bundle even when K8s is nil, got %v, %v", bBundle, err)
	}

	// Test local state store init failure
	cfgBadLocal := &config.Config{
		Runtime:       "docker",
		LocalStateDir: "/dev/null/impossible/dir",
	}
	ss, _, _ := buildDeclarativePlane(cfgBadLocal, st)
	if ss != nil {
		t.Errorf("expected nil state store when LocalStateDir is unwritable")
	}

	// Test unconfigured declarative plane
	cfgUnconfigured := &config.Config{}
	ssUnconf, recUnconf, committer := buildDeclarativePlane(cfgUnconfigured, st)
	if ssUnconf == nil || recUnconf == nil || committer != nil {
		t.Errorf("expected unconfigured state store and nil committer")
	}

	// Test buildAuth with invalid OIDC issuer (falls back to dev auth)
	cfgOIDC := &config.Config{
		OIDCIssuer: "http://invalid-oidc-issuer.example.com",
	}
	auth := buildAuth(context.Background(), cfgOIDC, st)
	if auth == nil {
		t.Errorf("expected dev auth fallback")
	}

	// Test buildAuth with empty store (no users, no OIDC) -> oidc.NewDev
	authDev := buildAuth(context.Background(), &config.Config{}, st)
	if authDev == nil {
		t.Errorf("expected dev auth fallback when store has no users")
	}
}

func TestWiring_ApplySyncLoops(t *testing.T) {
	origPoll := syncPollInterval
	origTimeout := syncTimeout
	defer func() {
		syncPollInterval = origPoll
		syncTimeout = origTimeout
	}()
	syncPollInterval = 5 * time.Millisecond
	syncTimeout = 30 * time.Millisecond

	// 1. makeApplyAfterSync with nil K8s -> no-op
	fnNil := makeApplyAfterSync(Deps{})
	fnNil("cm", "key", func(s string) bool { return true })

	// 2. makeApplyAfterSync with K8s and matching condition
	one := int32(1)
	cs := fakek8s.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim", Namespace: "valheim"},
			Spec:       appsv1.DeploymentSpec{Replicas: &one},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "valheim"},
			Data:       map[string]string{"foo": "bar"},
		},
	)
	k8sClient := k8s.NewWithClientset(cs, "valheim", "valheim")
	st, err := store.Open(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	d := Deps{
		K8s:   k8sClient,
		Store: st,
	}

	fn := makeApplyAfterSync(d)
	fn("test-cm", "foo", func(s string) bool { return s == "bar" })
	// Also test timeout branch
	fn("test-cm", "foo", func(s string) bool { return s == "never-match" })

	// 3. makeApplyMinecraftAfterSync
	fnMCNil := makeApplyMinecraftAfterSync(nil, Deps{})
	fnMCNil("cm", "dep", "key", func(s string) bool { return true })

	csMC := fakek8s.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "minecraft-modded", Namespace: "minecraft-modded"},
			Spec:       appsv1.DeploymentSpec{Replicas: &one},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "mc-cm", Namespace: "minecraft-modded"},
			Data:       map[string]string{"foo": "bar"},
		},
	)
	mcK8sClient := k8s.NewWithClientset(csMC, "minecraft-modded", "minecraft-modded")
	dMC := Deps{
		MCK8s: mcK8sClient,
		Store: st,
	}
	cfg := &config.Config{MinecraftDeployment: "minecraft-modded"}
	fnMC := makeApplyMinecraftAfterSync(cfg, dMC)
	fnMC("mc-cm", "minecraft-modded", "foo", func(s string) bool { return s == "bar" })
	fnMC("mc-cm", "", "foo", func(s string) bool { return s == "never-match" })

	// 4. makeApplyValheimAfterSync
	fnVhNil := makeApplyValheimAfterSync(nil, Deps{})
	fnVhNil("cm", "dep", "key", func(s string) bool { return true })

	fnVh := makeApplyValheimAfterSync(cfg, d)
	fnVh("test-cm", "valheim", "foo", func(s string) bool { return s == "bar" })
	fnVh("test-cm", "", "foo", func(s string) bool { return s == "never-match" })

	time.Sleep(50 * time.Millisecond)
}

func TestWiring_SchedulersAndBackfillEdgeCases(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "sched.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled context so schedulers immediately stop

	cfg := &config.Config{
		MinecraftNamespace: "minecraft-modded",
		ValheimNamespace:   "valheim",
		BackupsDir:         t.TempDir(),
	}

	d := Deps{
		Store: st,
	}

	// startMinecraftScheduler with nil MCInstances & MCRconPool
	startMinecraftScheduler(ctx, cfg, d)

	// startValheimScheduler with nil ValheimInstances
	startValheimScheduler(ctx, cfg, d)

	// incidentReader with nil Store
	nilIncidentFn := incidentReader(Deps{}, domain.GameMinecraft)
	if nilIncidentFn != nil {
		t.Errorf("expected nil incident reader when store is nil")
	}

	// incidentReader with non-nil Store
	incFn := incidentReader(Deps{Store: st}, domain.GameMinecraft)
	if incFn == nil {
		t.Fatalf("expected non-nil incident reader")
	}
	inc, err := incFn(context.Background(), 1)
	if err != nil || inc != nil {
		t.Errorf("expected nil incident, got %v, %v", inc, err)
	}
}

func TestWiring_BuildWizardHandler(t *testing.T) {
	// Set up mock ModpackIndex server
	mpiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "search") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"id":             123,
						"name":           "All The Mods",
						"summary":        "Big pack",
						"thumbnail_url":  "http://img/atm.png",
						"download_count": 50000,
						"links":          map[string]string{"curseforge": "https://curseforge.com/modpack/atm"},
					},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "modpack/123/mods") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"name": "jei", "forge": 1, "fabric": 0},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "modpack/123") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"id":    123,
					"links": map[string]string{"curseforge": "https://curseforge.com/modpack/atm"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer mpiSrv.Close()

	mpiClient := modpackindex.New(mpiSrv.URL)
	d := Deps{
		MPI: mpiClient,
	}

	h := buildWizardHandler(d)
	if h == nil {
		t.Fatal("expected wizard handler")
	}

	app := fiber.New()
	app.Post("/api/minecraft/wizard/modpacks", h.MCWizardModpacksSearch)

	form := url.Values{}
	form.Set("packQuery", "all the mods")
	req := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/modpacks", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestWiring_BackfillValheimSource_ErrorBranches(t *testing.T) {
	tempDir := t.TempDir()
	st, err := store.Open(filepath.Join(tempDir, "bf_err.db"))
	if err != nil {
		t.Fatal(err)
	}

	// 1. Closed store causes ValheimInstancesMissingSource to error
	stClosed, _ := store.Open(filepath.Join(tempDir, "closed.db"))
	_ = stClosed.Close()
	dClosed := &Deps{
		Store:            stClosed,
		K8s:              k8s.NewWithClientset(fakek8s.NewSimpleClientset(), "ns", "dep"),
		ValheimInstances: instances.NewInstanceManager(store.NewValheimInstanceRepo(stClosed), nil, nil, 10, 2, 1, "", "", nil, "ns"),
	}
	backfillValheimSource(context.Background(), dClosed)

	// 2. DeploymentEnv errors (missing deployment)
	valheimRepo := store.NewValheimInstanceRepo(st)
	_ = valheimRepo.Upsert(domain.Instance{
		Number: 99,
		Slug:   "missing-dep",
		GameID: domain.GameValheim,
		Source: "", // missing source!
	})
	dMissingDep := &Deps{
		Store:            st,
		K8s:              k8s.NewWithClientset(fakek8s.NewSimpleClientset(), "valheim", "valheim"),
		ValheimInstances: instances.NewInstanceManager(valheimRepo, nil, nil, 10, 2, 1, "", "", nil, "valheim"),
	}
	backfillValheimSource(context.Background(), dMissingDep)

	_ = st.Close()
}
