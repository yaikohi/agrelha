package console

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agrelha/internal/ports"

	"github.com/gofiber/fiber/v2"
)

type fakeRuntime struct {
	restarted bool
	stopped   bool
	started   bool
}

func (f *fakeRuntime) Start(ctx context.Context, ref ports.ServerRef) error {
	f.started = true
	return nil
}
func (f *fakeRuntime) Stop(ctx context.Context, ref ports.ServerRef) error {
	f.stopped = true
	return nil
}
func (f *fakeRuntime) Restart(ctx context.Context, ref ports.ServerRef) error {
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
	return io.NopCloser(strings.NewReader("log line\n")), nil
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
	}

	for _, ep := range endpoints {
		req := httptest.NewRequest(http.MethodPost, ep, nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s: %v", ep, err)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "Imperative plane disabled") {
			t.Errorf("%s response = %s, want Imperative plane disabled", ep, string(body))
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

	// 2. Stop (scale to 0)
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

	// 3. Start (scale to 1)
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

	if len(rec.audits) != 3 {
		t.Errorf("audits count = %d, want 3", len(rec.audits))
	}
	if len(rec.events) != 3 {
		t.Errorf("events count = %d, want 3", len(rec.events))
	}
}

func TestMCRconAndLogsUnconfigured(t *testing.T) {
	h := New(Config{})
	app := fiber.New()
	h.Register(app)

	// 1. RCON unconfigured
	reqRcon := httptest.NewRequest(http.MethodPost, "/api/minecraft/1/rcon", strings.NewReader("cmd=help"))
	reqRcon.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respRcon, err := app.Test(reqRcon)
	if err != nil {
		t.Fatal(err)
	}
	bodyRcon, _ := io.ReadAll(respRcon.Body)
	if !strings.Contains(string(bodyRcon), "unconfigured") {
		t.Errorf("rcon response = %s", string(bodyRcon))
	}

	// 2. Logs unconfigured
	reqLogs := httptest.NewRequest(http.MethodGet, "/api/minecraft/1/logs", nil)
	respLogs, err := app.Test(reqLogs)
	if err != nil {
		t.Fatal(err)
	}
	bodyLogs, _ := io.ReadAll(respLogs.Body)
	if !strings.Contains(string(bodyLogs), "Logs unavailable") {
		t.Errorf("logs response = %s", string(bodyLogs))
	}
}
