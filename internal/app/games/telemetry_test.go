package games_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"agrelha/internal/app/games"
	"agrelha/internal/ports"
)

type fakeTelemetryRuntime struct {
	status  ports.Status
	metrics ports.Metrics
}

func (f *fakeTelemetryRuntime) Start(context.Context, ports.ServerRef) error   { return nil }
func (f *fakeTelemetryRuntime) Stop(context.Context, ports.ServerRef) error    { return nil }
func (f *fakeTelemetryRuntime) Restart(context.Context, ports.ServerRef) error { return nil }
func (f *fakeTelemetryRuntime) Logs(context.Context, ports.ServerRef, ports.LogOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (f *fakeTelemetryRuntime) WatchAvailability(context.Context, ports.ServerRef, time.Duration) error {
	return nil
}

func (f *fakeTelemetryRuntime) Status(context.Context, ports.ServerRef) (ports.Status, error) {
	return f.status, nil
}

func (f *fakeTelemetryRuntime) Metrics(context.Context, ports.ServerRef) (ports.Metrics, error) {
	return f.metrics, nil
}

func TestCollectTelemetryNilRuntime(t *testing.T) {
	tele := games.CollectTelemetry(context.Background(), nil, ports.ServerRef{})
	if tele.Online {
		t.Fatalf("CollectTelemetry(nil) reported Online=true; dashboard will show offline server as active during midnight outage triage")
	}
	if tele.Lifecycle != "unknown" {
		t.Fatalf("CollectTelemetry(nil) Lifecycle = %q, want 'unknown'; operator cannot tell if status probe failed or server is booting", tele.Lifecycle)
	}
	if tele.CPU != "—" || tele.Memory != "—" || tele.Uptime != "—" {
		t.Fatalf("CollectTelemetry(nil) displayed metrics %+v instead of placeholder '—'; misleading empty stats rendered to users", tele)
	}
}

func TestCollectTelemetryActiveRuntime(t *testing.T) {
	started := time.Now().Add(-90 * time.Minute)
	rt := &fakeTelemetryRuntime{
		status: ports.Status{
			Lifecycle: ports.LifecycleRunning,
			Available: true,
			StartedAt: started,
		},
		metrics: ports.Metrics{
			CPUMillicores: 350,
			MemoryMiB:     2048,
			Known:         true,
		},
	}

	tele := games.CollectTelemetry(context.Background(), rt, ports.ServerRef{Name: "mc"})
	if !tele.Online || tele.Lifecycle != "running" {
		t.Fatalf("CollectTelemetry reported Online=%v Lifecycle=%q, want Online=true Lifecycle='running'; healthy server will be reported as down or have lifecycle corrupted to 'Up'", tele.Online, tele.Lifecycle)
	}
	if tele.CPU != "350m" {
		t.Fatalf("CollectTelemetry CPU = %q, want '350m'; operator cannot diagnose cpu spikes", tele.CPU)
	}
	if tele.Memory != "2048 Mi" {
		t.Fatalf("CollectTelemetry Memory = %q, want '2048 Mi'; operator cannot monitor ram pressure", tele.Memory)
	}
	if !strings.Contains(tele.Uptime, "1h") {
		t.Fatalf("CollectTelemetry Uptime = %q, want uptime containing '1h'; server uptime counter broken", tele.Uptime)
	}
}

func TestCollectTelemetryStartingRuntime(t *testing.T) {
	rt := &fakeTelemetryRuntime{
		status: ports.Status{
			Lifecycle: ports.LifecycleRunning,
			Available: false,
		},
	}

	tele := games.CollectTelemetry(context.Background(), rt, ports.ServerRef{Name: "mc"})
	if tele.Online {
		t.Fatalf("CollectTelemetry reported Online=true for starting runtime; players will attempt to connect before world generation finishes")
	}
	if tele.Lifecycle != "running" {
		t.Fatalf("CollectTelemetry Lifecycle = %q, want 'running'; operator at 2am will mistake world generation for a crash and trigger a restart loop", tele.Lifecycle)
	}
}

func TestCollectTelemetryStoppedRuntime(t *testing.T) {
	rt := &fakeTelemetryRuntime{
		status: ports.Status{
			Available: false,
			Lifecycle: ports.LifecycleStopped,
		},
	}

	tele := games.CollectTelemetry(context.Background(), rt, ports.ServerRef{Name: "valheim"})
	if tele.Online {
		t.Fatalf("CollectTelemetry reported Online=true for stopped runtime; dashboard will indicate stopped server is running")
	}
	if tele.Lifecycle != "stopped" {
		t.Fatalf("CollectTelemetry Lifecycle = %q, want 'stopped'; operator will not realize server has shut down", tele.Lifecycle)
	}
}
