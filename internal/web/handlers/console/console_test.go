package console

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"

	"github.com/gofiber/fiber/v2"
)

type fakeRuntime struct {
	restarted bool
	stopped   bool
	started   bool
	err       error
	logLines  string
}

func (f *fakeRuntime) Start(ctx context.Context, ref ports.ServerRef) error {
	if f.err != nil {
		return f.err
	}
	f.started = true
	return nil
}
func (f *fakeRuntime) Stop(ctx context.Context, ref ports.ServerRef) error {
	if f.err != nil {
		return f.err
	}
	f.stopped = true
	return nil
}
func (f *fakeRuntime) Restart(ctx context.Context, ref ports.ServerRef) error {
	if f.err != nil {
		return f.err
	}
	f.restarted = true
	return nil
}
func (f *fakeRuntime) Status(ctx context.Context, ref ports.ServerRef) (ports.Status, error) {
	return ports.Status{}, nil
}
func (f *fakeRuntime) Metrics(ctx context.Context, ref ports.ServerRef) (ports.Metrics, error) {
	return ports.Metrics{}, nil
}
func (f *fakeRuntime) Logs(ctx context.Context, ref ports.ServerRef, opts ports.LogOptions) (io.ReadCloser, error) {
	if f.err != nil {
		return nil, f.err
	}
	lines := f.logLines
	if lines == "" {
		lines = "log line\n"
	}
	return io.NopCloser(strings.NewReader(lines)), nil
}
func (f *fakeRuntime) WatchAvailability(ctx context.Context, ref ports.ServerRef, timeout time.Duration) error {
	return nil
}

var _ ports.Runtime = (*fakeRuntime)(nil)

type fakeRecorder struct {
	audits []string
	events []string
}

func (f *fakeRecorder) RecordAudit(actor, action, detail string) error {
	f.audits = append(f.audits, action)
	return nil
}
func (f *fakeRecorder) RecordEvent(kind, detail string) error {
	f.events = append(f.events, kind)
	return nil
}

func TestValheimConsolePage(t *testing.T) {
	h := New(Config{})
	app := fiber.New()
	app.Get("/valheim", h.ValheimConsole)

	req := httptest.NewRequest(http.MethodGet, "/valheim", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestServerLifecycleWithoutRuntime(t *testing.T) {
	h := New(Config{})
	app := fiber.New()
	h.Register(app)

	endpoints := []string{
		"/server/restart",
		"/server/update",
		"/server/stop",
		"/server/start",
		"/minecraft/server/restart",
		"/minecraft/server/stop",
		"/minecraft/server/start",
	}

	for _, ep := range endpoints {
		req := httptest.NewRequest(http.MethodPost, ep, nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s: %v", ep, err)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "disabled") {
			t.Errorf("%s response = %s, want disabled", ep, string(body))
		}
	}
}

func TestServerLifecycleWithRuntime(t *testing.T) {
	rt := &fakeRuntime{}
	rec := &fakeRecorder{}

	h := New(Config{
		ValheimRuntime: rt,
		ValheimRef:     ports.ServerRef{Name: "valheim", Scope: "valheim"},
		Audit:          rec,
		Event:          rec,
	})
	app := fiber.New()
	h.Register(app)

	// 1. Restart
	reqRestart := httptest.NewRequest(http.MethodPost, "/server/restart", nil)
	respRestart, err := app.Test(reqRestart)
	if err != nil {
		t.Fatal(err)
	}
	bodyRestart, _ := io.ReadAll(respRestart.Body)
	if !strings.Contains(string(bodyRestart), "Restart triggered") {
		t.Errorf("restart response = %s", string(bodyRestart))
	}
	if !rt.restarted {
		t.Error("expected runtime Restart to be called")
	}

	// 2. Update
	reqUpdate := httptest.NewRequest(http.MethodPost, "/server/update", nil)
	respUpdate, err := app.Test(reqUpdate)
	if err != nil {
		t.Fatal(err)
	}
	bodyUpdate, _ := io.ReadAll(respUpdate.Body)
	if !strings.Contains(string(bodyUpdate), "Update triggered") {
		t.Errorf("update response = %s", string(bodyUpdate))
	}

	// 3. Stop (scale to 0)
	reqStop := httptest.NewRequest(http.MethodPost, "/server/stop", nil)
	respStop, err := app.Test(reqStop)
	if err != nil {
		t.Fatal(err)
	}
	bodyStop, _ := io.ReadAll(respStop.Body)
	if !strings.Contains(string(bodyStop), "Stopping the server") {
		t.Errorf("stop response = %s", string(bodyStop))
	}
	if !rt.stopped {
		t.Error("expected runtime Stop to be called")
	}

	// 4. Start (scale to 1)
	reqStart := httptest.NewRequest(http.MethodPost, "/server/start", nil)
	respStart, err := app.Test(reqStart)
	if err != nil {
		t.Fatal(err)
	}
	bodyStart, _ := io.ReadAll(respStart.Body)
	if !strings.Contains(string(bodyStart), "Starting the server") {
		t.Errorf("start response = %s", string(bodyStart))
	}
	if !rt.started {
		t.Error("expected runtime Start to be called")
	}

	if len(rec.audits) != 4 {
		t.Errorf("audits count = %d, want 4", len(rec.audits))
	}
	if len(rec.events) != 4 {
		t.Errorf("events count = %d, want 4", len(rec.events))
	}
}

func TestServerLifecycleFailures(t *testing.T) {
	rt := &fakeRuntime{err: errors.New("boom")}
	h := New(Config{
		ValheimRuntime: rt,
		MCRuntime:      rt,
	})
	app := fiber.New()
	h.Register(app)

	endpoints := []string{
		"/server/restart",
		"/server/stop",
		"/server/start",
		"/minecraft/server/restart",
		"/minecraft/server/stop",
		"/minecraft/server/start",
	}

	for _, ep := range endpoints {
		req := httptest.NewRequest(http.MethodPost, ep, nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s: %v", ep, err)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "failed") {
			t.Errorf("%s response = %s, want failed", ep, string(body))
		}
	}
}

func TestMinecraftLifecycleWithRuntime(t *testing.T) {
	rt := &fakeRuntime{}
	rec := &fakeRecorder{}

	h := New(Config{
		MCRuntime: rt,
		MCRef:     ports.ServerRef{Name: "minecraft", Scope: "minecraft-modded"},
		Audit:     rec,
		Event:     rec,
	})
	app := fiber.New()
	h.Register(app)

	reqs := []struct {
		url     string
		wantMsg string
	}{
		{"/minecraft/server/restart", "Minecraft restart triggered"},
		{"/minecraft/server/stop", "Stopping Minecraft server"},
		{"/minecraft/server/start", "Starting Minecraft server"},
	}

	for _, tc := range reqs {
		req := httptest.NewRequest(http.MethodPost, tc.url, nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s: %v", tc.url, err)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), tc.wantMsg) {
			t.Errorf("%s response = %s, want %s", tc.url, string(body), tc.wantMsg)
		}
	}
}

func TestSSELogs(t *testing.T) {
	rt := &fakeRuntime{logLines: "server started\nplayer joined\n"}
	h := New(Config{
		ValheimRuntime: rt,
		MCRuntime:      rt,
	})
	app := fiber.New()
	h.Register(app)

	// Valheim logs stream
	reqVh := httptest.NewRequest(http.MethodGet, "/sse/logs?server=valheim", nil)
	respVh, err := app.Test(reqVh)
	if err != nil {
		t.Fatal(err)
	}
	bodyVh, _ := io.ReadAll(respVh.Body)
	if !strings.Contains(string(bodyVh), "server started") {
		t.Errorf("valheim logs = %s, want server started", string(bodyVh))
	}

	// Minecraft logs stream
	reqMC := httptest.NewRequest(http.MethodGet, "/sse/logs?server=minecraft", nil)
	respMC, err := app.Test(reqMC)
	if err != nil {
		t.Fatal(err)
	}
	bodyMC, _ := io.ReadAll(respMC.Body)
	if !strings.Contains(string(bodyMC), "player joined") {
		t.Errorf("mc logs = %s, want player joined", string(bodyMC))
	}

	// Runtime error on logs
	rtErr := &fakeRuntime{err: errors.New("cannot fetch logs")}
	hErr := New(Config{ValheimRuntime: rtErr})
	appErr := fiber.New()
	hErr.Register(appErr)
	respErr, _ := appErr.Test(httptest.NewRequest(http.MethodGet, "/sse/logs?server=valheim", nil))
	bodyErr, _ := io.ReadAll(respErr.Body)
	if len(bodyErr) != 0 {
		t.Errorf("expected empty body on log fetch error, got %s", string(bodyErr))
	}

	// Nil runtime
	hNil := New(Config{})
	appNil := fiber.New()
	hNil.Register(appNil)
	respNil, _ := appNil.Test(httptest.NewRequest(http.MethodGet, "/sse/logs?server=valheim", nil))
	bodyNil, _ := io.ReadAll(respNil.Body)
	if len(bodyNil) != 0 {
		t.Errorf("expected empty body on nil runtime, got %s", string(bodyNil))
	}
}

func TestMCRconAndLogs(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := store.NewInstanceRepo(st)
	_ = repo.Upsert(domain.Instance{
		Number: 1,
		Slug:   "ducktopia",
		Name:   "Ducktopia",
		State:  domain.StateRunning,
		GameID: domain.GameMinecraft,
	})

	rt := &fakeRuntime{logLines: "[Server] MC ready\n"}
	var executedCmd string
	var returnCmdErr error
	mcMgr := instances.NewInstanceManager(
		repo, nil, rt, 24, 4, 2, "manifests/minecraft-modded", "192.168.20.224", nil, "minecraft-modded",
		instances.WithCommandExecutor(func(ctx context.Context, inst domain.Instance, cmd string) (string, error) {
			executedCmd = cmd
			if returnCmdErr != nil {
				return "", returnCmdErr
			}
			return "Executed: " + cmd, nil
		}),
	)

	h := New(Config{
		MCInstances: mcMgr,
	})
	app := fiber.New()
	h.Register(app)

	// 1. Invalid number
	app.Post("/test/minecraft/:num/rcon", h.MCRconCommand)
	reqBadNum := httptest.NewRequest(http.MethodPost, "/test/minecraft/bad/rcon", strings.NewReader("cmd=help"))
	reqBadNum.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respBadNum, _ := app.Test(reqBadNum)
	bodyBadNum, _ := io.ReadAll(respBadNum.Body)
	if !strings.Contains(string(bodyBadNum), "Invalid instance number") {
		t.Errorf("bad num response = %s", string(bodyBadNum))
	}

	// 2. Empty command
	reqEmpty := httptest.NewRequest(http.MethodPost, "/api/minecraft/1/rcon", strings.NewReader("cmd=   "))
	reqEmpty.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respEmpty, _ := app.Test(reqEmpty)
	bodyEmpty, _ := io.ReadAll(respEmpty.Body)
	if !strings.Contains(string(bodyEmpty), "Command cannot be empty") {
		t.Errorf("empty cmd response = %s", string(bodyEmpty))
	}

	// 3. Successful command
	reqSuccess := httptest.NewRequest(http.MethodPost, "/api/minecraft/1/rcon", strings.NewReader("cmd=whitelist list"))
	reqSuccess.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respSuccess, _ := app.Test(reqSuccess)
	bodySuccess, _ := io.ReadAll(respSuccess.Body)
	if !strings.Contains(string(bodySuccess), "Executed: whitelist list") {
		t.Errorf("success response = %s", string(bodySuccess))
	}
	if executedCmd != "whitelist list" {
		t.Errorf("executed cmd = %s, want whitelist list", executedCmd)
	}

	// 4. Command executor returns error
	returnCmdErr = errors.New("rcon connection refused")
	reqErr := httptest.NewRequest(http.MethodPost, "/api/minecraft/1/rcon", strings.NewReader("cmd=say hi"))
	reqErr.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respErr, _ := app.Test(reqErr)
	bodyErr, _ := io.ReadAll(respErr.Body)
	if !strings.Contains(string(bodyErr), "Error: rcon connection refused") {
		t.Errorf("error response = %s", string(bodyErr))
	}

	// 5. MCLogsStream: invalid number
	app.Get("/test/minecraft/:num/logs", h.MCLogsStream)
	reqBadLogs := httptest.NewRequest(http.MethodGet, "/test/minecraft/bad/logs", nil)
	respBadLogs, _ := app.Test(reqBadLogs)
	if respBadLogs.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for bad logs number, got %d", respBadLogs.StatusCode)
	}

	// 6. MCLogsStream: success
	reqMCLogs := httptest.NewRequest(http.MethodGet, "/api/minecraft/1/logs", nil)
	respMCLogs, _ := app.Test(reqMCLogs)
	bodyMCLogs, _ := io.ReadAll(respMCLogs.Body)
	if !strings.Contains(string(bodyMCLogs), "[Server] MC ready") {
		t.Errorf("logs stream response = %s", string(bodyMCLogs))
	}

	// 7. MCLogsStream: runtime error
	rt.err = errors.New("pod not running")
	reqMCLogsErr := httptest.NewRequest(http.MethodGet, "/api/minecraft/1/logs", nil)
	respMCLogsErr, _ := app.Test(reqMCLogsErr)
	bodyMCLogsErr, _ := io.ReadAll(respMCLogsErr.Body)
	if !strings.Contains(string(bodyMCLogsErr), "Log stream error: pod not running") {
		t.Errorf("logs stream err response = %s", string(bodyMCLogsErr))
	}
}

func TestValheimLogsStream(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := store.NewValheimInstanceRepo(st)
	_ = repo.Upsert(domain.Instance{
		Number: 1,
		Slug:   "valheim-01",
		Name:   "Valheim 01",
		State:  domain.StateRunning,
		GameID: domain.GameValheim,
	})

	rt := &fakeRuntime{logLines: "Valheim server started\nGame engine init\n"}
	vhMgr := instances.NewInstanceManager(
		repo, nil, rt, 16, 4, 2, "manifests/valheim", "192.168.20.224", nil, "valheim",
		instances.WithGameID(domain.GameValheim),
	)

	h := New(Config{
		ValheimInstances: vhMgr,
	})
	app := fiber.New()
	h.Register(app)

	// 1. Invalid number
	app.Get("/test/valheim/:num/logs", h.ValheimLogsStream)
	reqBad := httptest.NewRequest(http.MethodGet, "/test/valheim/bad/logs", nil)
	respBad, _ := app.Test(reqBad)
	if respBad.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for bad valheim log number, got %d", respBad.StatusCode)
	}

	// 2. Stream success
	reqLogs := httptest.NewRequest(http.MethodGet, "/api/valheim/1/logs", nil)
	respLogs, _ := app.Test(reqLogs)
	bodyLogs, _ := io.ReadAll(respLogs.Body)
	if !strings.Contains(string(bodyLogs), "Valheim server started") {
		t.Errorf("valheim stream response = %s", string(bodyLogs))
	}

	// 3. Stream error
	rt.err = errors.New("container not ready")
	reqErr := httptest.NewRequest(http.MethodGet, "/api/valheim/1/logs", nil)
	respErr, _ := app.Test(reqErr)
	bodyErr, _ := io.ReadAll(respErr.Body)
	if !strings.Contains(string(bodyErr), "Log stream error: container not ready") {
		t.Errorf("valheim stream err response = %s", string(bodyErr))
	}

	// 4. Unconfigured instances
	hUnconf := New(Config{})
	appUnconf := fiber.New()
	hUnconf.Register(appUnconf)
	respUnconf, _ := appUnconf.Test(httptest.NewRequest(http.MethodGet, "/api/valheim/1/logs", nil))
	bodyUnconf, _ := io.ReadAll(respUnconf.Body)
	if !strings.Contains(string(bodyUnconf), "Logs unavailable") {
		t.Errorf("valheim unconf response = %s", string(bodyUnconf))
	}
}
