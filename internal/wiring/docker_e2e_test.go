package wiring_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	valheimmanifests "agrelha/internal/infra/manifests/valheim"
	composereconcile "agrelha/internal/infra/reconcile/compose"
	localstate "agrelha/internal/infra/state/local"
	"agrelha/internal/infra/store"
	"agrelha/internal/platform/config"
	"agrelha/internal/ports"
	"agrelha/internal/web/handlers/console"
	"agrelha/internal/wiring"
)

type mockDockerRuntime struct {
	ports.Runtime
	logsReader io.ReadCloser
	logsErr    error
	logsCtx    context.Context
}

func (m *mockDockerRuntime) Logs(ctx context.Context, ref ports.ServerRef, opts ports.LogOptions) (io.ReadCloser, error) {
	m.logsCtx = ctx
	if m.logsErr != nil {
		return nil, m.logsErr
	}
	if m.logsReader != nil {
		return m.logsReader, nil
	}
	return io.NopCloser(strings.NewReader("server log line 1\nserver log line 2\n")), nil
}

func (m *mockDockerRuntime) Start(ctx context.Context, ref ports.ServerRef) error {
	return nil
}

func (m *mockDockerRuntime) Stop(ctx context.Context, ref ports.ServerRef) error {
	return nil
}

func (m *mockDockerRuntime) Restart(ctx context.Context, ref ports.ServerRef) error {
	return nil
}

func (m *mockDockerRuntime) Status(ctx context.Context, ref ports.ServerRef) (ports.Status, error) {
	return ports.Status{Available: true}, nil
}

// TestDocker_StartExistingServer_PreservesModsAndStateStoreMigration tests that:
// 1. A server created before the state store migration has its legacy compose/valheim/instance-01/mods.yaml read via fallback.
// 2. StartInstance succeeds without failing on missing mods.yaml.
// 3. The server state in DB transitions to 'running'.
// 4. State is properly migrated into the new LocalStateDir, preserving all authored mods.
// 5. Stopping the server transitions state back to 'stopped'.
func TestDocker_StartExistingServer_PreservesModsAndStateStoreMigration(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "agrelha.db")
	composeDir := filepath.Join(tmp, "compose")
	stateDir := filepath.Join(tmp, "state")
	dataDir := filepath.Join(tmp, "data")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Seed valheim instance 1 as stopped on host "fogo"
	vhRepo := store.NewValheimInstanceRepo(st)
	if err := vhRepo.Upsert(domain.Instance{
		Number: 1,
		Slug:   "lareira-v2",
		Name:   "lareira-V2",
		State:  domain.StateStopped,
		Host:   "fogo",
		GameID: domain.GameValheim,
	}); err != nil {
		t.Fatal(err)
	}

	// Create legacy compose files in composeDir (pre-dating state store migration)
	legacyInstanceDir := filepath.Join(composeDir, "valheim", "instance-01")
	if err := os.MkdirAll(legacyInstanceDir, 0755); err != nil {
		t.Fatal(err)
	}
	legacyModsYAML := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: valheim-lareira-v2-01-mods\ndata:\n  mods.txt: |\n    MidnightMods/AchievementEnabler/0.5.2\n    Advize/PlantEverything/1.21.4\n"
	if err := os.WriteFile(filepath.Join(legacyInstanceDir, "mods.yaml"), []byte(legacyModsYAML), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyInstanceDir, "docker-compose.yml"), []byte("services: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Intercept compose executor so docker command does not execute real system docker
	var executedCmds []string
	origExec := composereconcile.DefaultExecutor
	defer func() { composereconcile.DefaultExecutor = origExec }()
	composereconcile.DefaultExecutor = func(ctx context.Context, workDir string, args ...string) error {
		executedCmds = append(executedCmds, strings.Join(args, " "))
		return nil
	}

	// Mock Docker unix socket to accept container stop requests
	dockerSock := filepath.Join(tmp, "docker.sock")
	l, err := net.Listen("unix", dockerSock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	dockerSrv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}
	go func() { _ = dockerSrv.Serve(l) }()
	defer func() { _ = dockerSrv.Close() }()

	cfg := &config.Config{
		DBPath:                dbPath,
		AuthMode:              "dev",
		Runtime:               "docker",
		DefaultHost:           "fogo",
		DockerHostName:        "fogo",
		DockerSocket:          dockerSock,
		ComposeDir:            composeDir,
		LocalStateDir:         stateDir,
		DataDir:               dataDir,
		ValheimTotalBudgetGiB: 16,
		ValheimMaxInstances:   4,
		ValheimMaxRunning:     2,
		ValheimInstancesPath:  "manifests/valheim",
	}

	deps, err := wiring.Build(context.Background(), cfg)
	if err != nil {
		t.Fatalf("wiring.Build: %v", err)
	}
	defer func() { _ = deps.Store.Close() }()
	deps.Auth = testAdminAuth{}

	server := wiring.BuildServer(context.Background(), cfg, deps)

	// 1. POST /api/valheim/instances/1/start
	reqStart := httptest.NewRequest(fiber.MethodPost, "/api/valheim/instances/1/start", nil)
	respStart, err := server.Test(reqStart)
	if err != nil {
		t.Fatalf("start request failed: %v", err)
	}
	if respStart.StatusCode != fiber.StatusOK {
		t.Fatalf("start status = %d, want 200", respStart.StatusCode)
	}
	bodyStart, _ := io.ReadAll(respStart.Body)
	if !strings.Contains(string(bodyStart), "Starting Valheim instance #01...") {
		t.Fatalf("unexpected start response body: %s", string(bodyStart))
	}
	if strings.Contains(string(bodyStart), "Start failed") {
		t.Fatalf("start returned error toast: %s", string(bodyStart))
	}

	// 2. Verify instance state in DB is now 'running'
	inst1, err := vhRepo.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if inst1.State != domain.StateRunning {
		t.Errorf("instance 1 state = %q, want 'running'", inst1.State)
	}

	// 3. Verify reconciler executed compose up
	if len(executedCmds) == 0 {
		t.Fatal("expected compose reconciler to execute docker compose up")
	}
	if !strings.Contains(executedCmds[0], "up -d") {
		t.Errorf("unexpected compose args: %s", executedCmds[0])
	}

	// 4. Verify state store migration: mods.yaml must now exist in stateDir and contain the 2 mods
	stateModsPath := filepath.Join(stateDir, "compose", "valheim", "instance-01", "mods.yaml")
	stateModsContent, err := os.ReadFile(stateModsPath)
	if err != nil {
		t.Fatalf("expected state store to contain migrated mods.yaml at %s: %v", stateModsPath, err)
	}
	if !strings.Contains(string(stateModsContent), "MidnightMods/AchievementEnabler/0.5.2") {
		t.Errorf("migrated mods.yaml missing AchievementEnabler: %s", string(stateModsContent))
	}
	if !strings.Contains(string(stateModsContent), "Advize/PlantEverything/1.21.4") {
		t.Errorf("migrated mods.yaml missing PlantEverything: %s", string(stateModsContent))
	}

	// 5. POST /api/valheim/instances/1/stop
	reqStop := httptest.NewRequest(fiber.MethodPost, "/api/valheim/instances/1/stop", nil)
	respStop, err := server.Test(reqStop)
	if err != nil {
		t.Fatalf("stop request failed: %v", err)
	}
	if respStop.StatusCode != fiber.StatusOK {
		t.Fatalf("stop status = %d, want 200", respStop.StatusCode)
	}
	bodyStop, _ := io.ReadAll(respStop.Body)
	if strings.Contains(string(bodyStop), "Stop failed") {
		t.Fatalf("stop returned error: %s", string(bodyStop))
	}
	inst1Stopped, _ := vhRepo.Get(1)
	if inst1Stopped.State != domain.StateStopped {
		t.Errorf("instance 1 state after stop = %q, want 'stopped'", inst1Stopped.State)
	}
}

// TestDocker_StartServer_WithoutExistingModsYaml_Succeeds tests that a world without any mods.yaml
// starts cleanly without failing on missing file errors.
func TestDocker_StartServer_WithoutExistingModsYaml_Succeeds(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "agrelha.db")
	composeDir := filepath.Join(tmp, "compose")
	stateDir := filepath.Join(tmp, "state")
	dataDir := filepath.Join(tmp, "data")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Seed instance 2 with no pre-existing mods file anywhere
	vhRepo := store.NewValheimInstanceRepo(st)
	if err := vhRepo.Upsert(domain.Instance{
		Number: 2,
		Slug:   "boppo",
		Name:   "boppo",
		State:  domain.StateStopped,
		Host:   "fogo",
		GameID: domain.GameValheim,
	}); err != nil {
		t.Fatal(err)
	}

	origExec := composereconcile.DefaultExecutor
	defer func() { composereconcile.DefaultExecutor = origExec }()
	composereconcile.DefaultExecutor = func(ctx context.Context, workDir string, args ...string) error {
		return nil
	}

	cfg := &config.Config{
		DBPath:                dbPath,
		AuthMode:              "dev",
		Runtime:               "docker",
		DefaultHost:           "fogo",
		DockerHostName:        "fogo",
		ComposeDir:            composeDir,
		LocalStateDir:         stateDir,
		DataDir:               dataDir,
		ValheimTotalBudgetGiB: 16,
		ValheimMaxInstances:   4,
		ValheimMaxRunning:     2,
		ValheimInstancesPath:  "manifests/valheim",
	}

	deps, err := wiring.Build(context.Background(), cfg)
	if err != nil {
		t.Fatalf("wiring.Build: %v", err)
	}
	defer func() { _ = deps.Store.Close() }()
	deps.Auth = testAdminAuth{}

	server := wiring.BuildServer(context.Background(), cfg, deps)

	// POST /api/valheim/instances/2/start must succeed without crashing on missing mods.yaml
	reqStart := httptest.NewRequest(fiber.MethodPost, "/api/valheim/instances/2/start", nil)
	respStart, err := server.Test(reqStart)
	if err != nil {
		t.Fatalf("start request failed: %v", err)
	}
	if respStart.StatusCode != fiber.StatusOK {
		t.Fatalf("start status = %d, want 200", respStart.StatusCode)
	}
	bodyStart, _ := io.ReadAll(respStart.Body)
	if strings.Contains(string(bodyStart), "Start failed") {
		t.Fatalf("start returned error: %s", string(bodyStart))
	}
	if !strings.Contains(string(bodyStart), "Starting Valheim instance #02...") {
		t.Fatalf("unexpected response: %s", string(bodyStart))
	}

	inst2, _ := vhRepo.Get(2)
	if inst2.State != domain.StateRunning {
		t.Errorf("instance 2 state = %q, want 'running'", inst2.State)
	}
}

// TestDocker_ConsoleLogsStream_ContainerNotFound_ReturnsFriendlyMessage tests that when a stopped
// server's container does not exist in Docker (returning 404 / ErrServerNotFound), the SSE stream returns
// a polite user-friendly message instead of a raw Docker 404 error badge.
func TestDocker_ConsoleLogsStream_ContainerNotFound_ReturnsFriendlyMessage(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "agrelha.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Seed instance 2 (valheim-boppo-02) as stopped
	vhRepo := store.NewValheimInstanceRepo(st)
	if err := vhRepo.Upsert(domain.Instance{
		Number: 2,
		Slug:   "boppo",
		Name:   "boppo",
		State:  domain.StateStopped,
		Host:   "fogo",
		GameID: domain.GameValheim,
	}); err != nil {
		t.Fatal(err)
	}

	mockRT := &mockDockerRuntime{
		logsErr: ports.ErrServerNotFound,
	}

	localSS, err := localstate.New(filepath.Join(tmp, "state"))
	if err != nil {
		t.Fatal(err)
	}

	rec, err := composereconcile.New(composereconcile.WithWorkDir(filepath.Join(tmp, "compose")))
	if err != nil {
		t.Fatal(err)
	}

	vhMgr := instances.NewInstanceManager(
		vhRepo, localSS, mockRT, 16, 4, 2, "manifests/valheim", "192.168.20.224",
		valheimmanifests.NewComposeRenderer("", filepath.Join(tmp, "data")), "valheim",
		instances.WithGameID(domain.GameValheim),
		instances.WithReconciler(rec),
	)

	h := console.New(console.Config{
		ValheimInstances: vhMgr,
	})
	app := fiber.New()
	h.Register(app)

	// GET /api/valheim/2/logs/stream
	reqLogs := httptest.NewRequest(fiber.MethodGet, "/api/valheim/2/logs/stream", nil)
	respLogs, err := app.Test(reqLogs)
	if err != nil {
		t.Fatalf("logs stream request failed: %v", err)
	}
	bodyLogs, _ := io.ReadAll(respLogs.Body)
	respStr := string(bodyLogs)

	// Must contain clean friendly message
	if !strings.Contains(respStr, "Server is not running. Start the server to generate logs.") {
		t.Errorf("expected friendly stopped message, got: %s", respStr)
	}
	// Must NOT contain raw 404 or "Log stream error"
	if strings.Contains(respStr, "Log stream error") || strings.Contains(respStr, "status 404") {
		t.Errorf("response contains raw error message: %s", respStr)
	}
}

// TestDocker_ConsoleLogsStream_RunningServer_StreamsContinuously tests that a running server's
// logs stream over SSE and the context is not prematurely canceled when the HTTP handler returns.
func TestDocker_ConsoleLogsStream_RunningServer_StreamsContinuously(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "agrelha.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	vhRepo := store.NewValheimInstanceRepo(st)
	if err := vhRepo.Upsert(domain.Instance{
		Number: 1,
		Slug:   "lareira-v2",
		Name:   "lareira-V2",
		State:  domain.StateRunning,
		Host:   "fogo",
		GameID: domain.GameValheim,
	}); err != nil {
		t.Fatal(err)
	}

	mockRT := &mockDockerRuntime{
		logsReader: io.NopCloser(strings.NewReader("valheim server started\nready for connections\n")),
	}

	localSS, err := localstate.New(filepath.Join(tmp, "state"))
	if err != nil {
		t.Fatal(err)
	}

	rec, err := composereconcile.New(composereconcile.WithWorkDir(filepath.Join(tmp, "compose")))
	if err != nil {
		t.Fatal(err)
	}

	vhMgr := instances.NewInstanceManager(
		vhRepo, localSS, mockRT, 16, 4, 2, "manifests/valheim", "192.168.20.224",
		valheimmanifests.NewComposeRenderer("", filepath.Join(tmp, "data")), "valheim",
		instances.WithGameID(domain.GameValheim),
		instances.WithReconciler(rec),
	)

	h := console.New(console.Config{
		ValheimInstances: vhMgr,
	})
	app := fiber.New()
	h.Register(app)

	// GET /api/valheim/1/logs/stream
	reqLogs := httptest.NewRequest(fiber.MethodGet, "/api/valheim/1/logs/stream", nil)
	respLogs, err := app.Test(reqLogs)
	if err != nil {
		t.Fatalf("logs stream request failed: %v", err)
	}
	bodyLogs, _ := io.ReadAll(respLogs.Body)
	respStr := string(bodyLogs)

	if !strings.Contains(respStr, "valheim server started") || !strings.Contains(respStr, "ready for connections") {
		t.Errorf("logs stream missing expected content: %s", respStr)
	}

	// Verify that the context passed to the runtime Logs reader was cleaned up upon stream completion
	time.Sleep(10 * time.Millisecond)
	if mockRT.logsCtx == nil {
		t.Error("expected logsCtx to be captured")
	} else if mockRT.logsCtx.Err() == nil {
		t.Error("expected logsCtx to be canceled after stream writer completed")
	}
}
