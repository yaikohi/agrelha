package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"agrelha/internal/ports"
)

type mockClient struct {
	started   string
	stopped   string
	restarted string
	inspect   ContainerInspect
	stats     ContainerStats
	logsData  string
	err       error
}

func (m *mockClient) ContainerStart(ctx context.Context, id string) error {
	m.started = id
	return m.err
}

func (m *mockClient) ContainerStop(ctx context.Context, id string) error {
	m.stopped = id
	return m.err
}

func (m *mockClient) ContainerRestart(ctx context.Context, id string) error {
	m.restarted = id
	return m.err
}

func (m *mockClient) ContainerInspect(ctx context.Context, id string) (ContainerInspect, error) {
	if m.err != nil {
		return ContainerInspect{}, m.err
	}
	return m.inspect, nil
}

func (m *mockClient) ContainerStats(ctx context.Context, id string) (ContainerStats, error) {
	if m.err != nil {
		return ContainerStats{}, m.err
	}
	return m.stats, nil
}

func (m *mockClient) ContainerLogs(ctx context.Context, id string, opts ports.LogOptions) (io.ReadCloser, error) {
	if m.err != nil {
		return nil, m.err
	}
	return io.NopCloser(strings.NewReader(m.logsData)), nil
}

func TestDockerRuntime_StartStopRestart(t *testing.T) {
	ctx := context.Background()
	mock := &mockClient{}
	r := New(WithClient(mock))

	ref := ports.ServerRef{Name: "mc-ayyy-01"}

	if err := r.Start(ctx, ref); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if mock.started != "mc-ayyy-01" {
		t.Errorf("mock.started = %q, want 'mc-ayyy-01'", mock.started)
	}

	if err := r.Stop(ctx, ref); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if mock.stopped != "mc-ayyy-01" {
		t.Errorf("mock.stopped = %q, want 'mc-ayyy-01'", mock.stopped)
	}

	if err := r.Restart(ctx, ref); err != nil {
		t.Fatalf("Restart failed: %v", err)
	}
	if mock.restarted != "mc-ayyy-01" {
		t.Errorf("mock.restarted = %q, want 'mc-ayyy-01'", mock.restarted)
	}
}

func TestDockerRuntime_StatusDerivation(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	tests := []struct {
		name      string
		inspect   ContainerInspect
		err       error
		wantLife  ports.Lifecycle
		wantAvail bool
	}{
		{
			name: "running and healthy",
			inspect: ContainerInspect{
				State: ContainerState{
					Running:   true,
					StartedAt: now,
					Health: &struct {
						Status string `json:"Status"`
					}{Status: "healthy"},
				},
			},
			wantLife:  ports.LifecycleRunning,
			wantAvail: true,
		},
		{
			name: "running but unhealthy",
			inspect: ContainerInspect{
				State: ContainerState{
					Running:   true,
					StartedAt: now,
					Health: &struct {
						Status string `json:"Status"`
					}{Status: "unhealthy"},
				},
			},
			wantLife:  ports.LifecycleRunning,
			wantAvail: false,
		},
		{
			name: "running without healthcheck",
			inspect: ContainerInspect{
				State: ContainerState{
					Running:   true,
					StartedAt: now,
				},
			},
			wantLife:  ports.LifecycleRunning,
			wantAvail: true,
		},
		{
			name: "stopped / exited",
			inspect: ContainerInspect{
				State: ContainerState{
					Status:  "exited",
					Running: false,
				},
			},
			wantLife:  ports.LifecycleStopped,
			wantAvail: false,
		},
		{
			name:      "container not found (404)",
			err:       os.ErrNotExist,
			wantLife:  ports.LifecycleStopped,
			wantAvail: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockClient{inspect: tc.inspect, err: tc.err}
			r := New(WithClient(mock))

			st, err := r.Status(ctx, ports.ServerRef{Name: "test"})
			if tc.err != nil && !errors.Is(tc.err, os.ErrNotExist) {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Status error: %v", err)
			}
			if st.Lifecycle != tc.wantLife {
				t.Errorf("Lifecycle = %v, want %v", st.Lifecycle, tc.wantLife)
			}
			if st.Available != tc.wantAvail {
				t.Errorf("Available = %v, want %v", st.Available, tc.wantAvail)
			}
		})
	}
}

func TestDockerRuntime_MetricsCalculation(t *testing.T) {
	ctx := context.Background()
	mock := &mockClient{
		stats: ContainerStats{
			MemoryStats: struct {
				Usage int64 `json:"usage"`
				Limit int64 `json:"limit"`
			}{
				Usage: 2 * 1024 * 1024 * 1024, // 2048 MiB
			},
			CPUStats: struct {
				CPUUsage struct {
					TotalUsage uint64 `json:"total_usage"`
				} `json:"cpu_usage"`
				SystemCPUUsage uint64 `json:"system_cpu_usage"`
				OnlineCPUs     uint32 `json:"online_cpus"`
			}{
				CPUUsage: struct {
					TotalUsage uint64 `json:"total_usage"`
				}{TotalUsage: 200000000},
				SystemCPUUsage: 1000000000,
				OnlineCPUs:     2,
			},
			PreCPUStats: struct {
				CPUUsage struct {
					TotalUsage uint64 `json:"total_usage"`
				} `json:"cpu_usage"`
				SystemCPUUsage uint64 `json:"system_cpu_usage"`
			}{
				CPUUsage: struct {
					TotalUsage uint64 `json:"total_usage"`
				}{TotalUsage: 100000000},
				SystemCPUUsage: 800000000,
			},
		},
	}
	r := New(WithClient(mock))

	m, err := r.Metrics(ctx, ports.ServerRef{Name: "test"})
	if err != nil {
		t.Fatalf("Metrics failed: %v", err)
	}

	if !m.Known {
		t.Errorf("Metrics.Known should be true")
	}
	if m.MemoryMiB != 2048 {
		t.Errorf("MemoryMiB = %d, want 2048", m.MemoryMiB)
	}
	// cpuDelta = 100000000, sysDelta = 200000000, 2 CPUs -> 0.5 * 2 * 1000 = 1000 millicores (1 core)
	if m.CPUMillicores != 1000 {
		t.Errorf("CPUMillicores = %d, want 1000", m.CPUMillicores)
	}
}

func TestDockerRuntime_Logs(t *testing.T) {
	ctx := context.Background()
	mock := &mockClient{logsData: "log line 1\nlog line 2\n"}
	r := New(WithClient(mock))

	rc, err := r.Logs(ctx, ports.ServerRef{Name: "test"}, ports.LogOptions{Tail: 50})
	if err != nil {
		t.Fatalf("Logs failed: %v", err)
	}
	defer rc.Close()

	out, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read logs: %v", err)
	}
	if string(out) != "log line 1\nlog line 2\n" {
		t.Errorf("got logs %q, want 'log line 1\nlog line 2\n'", string(out))
	}
}

func TestDemuxLogReader(t *testing.T) {
	var raw bytes.Buffer
	// Frame 1: stdout, 5 bytes "hello"
	var hdr1 [8]byte
	hdr1[0] = 1 // stdout
	binary.BigEndian.PutUint32(hdr1[4:8], 5)
	raw.Write(hdr1[:])
	raw.WriteString("hello")

	// Frame 2: stderr, 6 bytes " world"
	var hdr2 [8]byte
	hdr2[0] = 2 // stderr
	binary.BigEndian.PutUint32(hdr2[4:8], 6)
	raw.Write(hdr2[:])
	raw.WriteString(" world")

	reader := newDemuxLogReader(io.NopCloser(&raw))
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read demux logs: %v", err)
	}
	if string(out) != "hello world" {
		t.Errorf("demux output = %q, want 'hello world'", string(out))
	}
}

func TestSocketClient_OverHTTP(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()

	mux.HandleFunc("/v1.43/containers/c1/start", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/v1.43/containers/c1/stop", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/v1.43/containers/c1/restart", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/v1.43/containers/c1/json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ContainerInspect{
			ID: "c1",
			State: ContainerState{
				Running: true,
				Status:  "running",
			},
		})
	})
	mux.HandleFunc("/v1.43/containers/c1/stats", func(w http.ResponseWriter, r *http.Request) {
		var stats ContainerStats
		stats.MemoryStats.Usage = 104857600
		_ = json.NewEncoder(w).Encode(stats)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewSocketClient("",
		WithHTTPClient(srv.Client()),
		WithBaseURL(srv.URL),
	)

	if err := client.ContainerStart(ctx, "c1"); err != nil {
		t.Errorf("ContainerStart: %v", err)
	}
	if err := client.ContainerStop(ctx, "c1"); err != nil {
		t.Errorf("ContainerStop: %v", err)
	}
	if err := client.ContainerRestart(ctx, "c1"); err != nil {
		t.Errorf("ContainerRestart: %v", err)
	}
	inspect, err := client.ContainerInspect(ctx, "c1")
	if err != nil || !inspect.State.Running {
		t.Errorf("ContainerInspect: %+v, err: %v", inspect, err)
	}
	stats, err := client.ContainerStats(ctx, "c1")
	if err != nil || stats.MemoryStats.Usage != 104857600 {
		t.Errorf("ContainerStats: %+v, err: %v", stats, err)
	}
}

func TestDockerRuntime_NilReceiverAndClient(t *testing.T) {
	ctx := context.Background()
	ref := ports.ServerRef{Name: "c1"}

	check := func(name string, r *Runtime) {
		t.Run(name, func(t *testing.T) {
			if err := r.Start(ctx, ref); err != ports.ErrNotImplemented {
				t.Fatalf("Start want ErrNotImplemented, got %v", err)
			}
			if err := r.Stop(ctx, ref); err != ports.ErrNotImplemented {
				t.Fatalf("Stop want ErrNotImplemented, got %v", err)
			}
			if err := r.Restart(ctx, ref); err != ports.ErrNotImplemented {
				t.Fatalf("Restart want ErrNotImplemented, got %v", err)
			}
			st, err := r.Status(ctx, ref)
			if err != ports.ErrNotImplemented || st.Lifecycle != ports.LifecycleUnknown {
				t.Fatalf("Status want ErrNotImplemented, got %v, %+v", err, st)
			}
			if _, err := r.Metrics(ctx, ref); err != ports.ErrNotImplemented {
				t.Fatalf("Metrics want ErrNotImplemented, got %v", err)
			}
			if _, err := r.Logs(ctx, ref, ports.LogOptions{}); err != ports.ErrNotImplemented {
				t.Fatalf("Logs want ErrNotImplemented, got %v", err)
			}
			if err := r.WatchAvailability(ctx, ref, time.Millisecond); err != ports.ErrNotImplemented {
				t.Fatalf("WatchAvailability want ErrNotImplemented, got %v", err)
			}
		})
	}

	check("nil receiver", nil)
	check("nil client", &Runtime{client: nil})
}

func TestDockerRuntime_New_DefaultClient(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	r := New()
	if r == nil || r.client == nil {
		t.Fatal("expected non-nil runtime and client")
	}
}

func TestDockerRuntime_Status_EdgeCases(t *testing.T) {
	ctx := context.Background()
	ref := ports.ServerRef{Name: "c1"}

	// Error from inspect other than ErrNotExist
	mockErr := &mockClient{err: errors.New("docker daemon down")}
	rErr := New(WithClient(mockErr))
	st, err := rErr.Status(ctx, ref)
	if err == nil || st.Lifecycle != ports.LifecycleUnknown {
		t.Fatalf("expected error on daemon failure, got %v, st=%+v", err, st)
	}

	// Health status is "none"
	mockNone := &mockClient{
		inspect: ContainerInspect{
			State: ContainerState{
				Running: true,
				Health: &struct {
					Status string `json:"Status"`
				}{Status: "none"},
				StartedAt: "invalid-time",
			},
		},
	}
	rNone := New(WithClient(mockNone))
	stNone, err := rNone.Status(ctx, ref)
	if err != nil {
		t.Fatalf("Status unexpected error: %v", err)
	}
	if !stNone.Available {
		t.Errorf("when health status is none, Available should fallback to Running (true)")
	}
	if !stNone.StartedAt.IsZero() {
		t.Errorf("StartedAt with invalid time should be zero")
	}
}

func TestDockerRuntime_Metrics_EdgeCases(t *testing.T) {
	ctx := context.Background()
	ref := ports.ServerRef{Name: "c1"}

	// Error from ContainerStats
	mockErr := &mockClient{err: errors.New("stats error")}
	rErr := New(WithClient(mockErr))
	if _, err := rErr.Metrics(ctx, ref); err == nil {
		t.Fatalf("expected error from Metrics, got nil")
	}

	// Zero memory and zero cpu delta
	mockZero := &mockClient{
		stats: ContainerStats{},
	}
	rZero := New(WithClient(mockZero))
	m, err := rZero.Metrics(ctx, ref)
	if err != nil {
		t.Fatalf("Metrics unexpected error: %v", err)
	}
	if m.MemoryMiB != 0 || m.CPUMillicores != 0 {
		t.Errorf("expected 0 memory and 0 cpu, got %+v", m)
	}

	// CPUs == 0 fallback to 1
	mockCpus0 := &mockClient{
		stats: ContainerStats{
			MemoryStats: struct {
				Usage int64 `json:"usage"`
				Limit int64 `json:"limit"`
			}{Usage: 50 * 1024 * 1024},
			CPUStats: struct {
				CPUUsage struct {
					TotalUsage uint64 `json:"total_usage"`
				} `json:"cpu_usage"`
				SystemCPUUsage uint64 `json:"system_cpu_usage"`
				OnlineCPUs     uint32 `json:"online_cpus"`
			}{
				CPUUsage: struct {
					TotalUsage uint64 `json:"total_usage"`
				}{TotalUsage: 500},
				SystemCPUUsage: 1000,
				OnlineCPUs:     0, // Will fallback to 1
			},
			PreCPUStats: struct {
				CPUUsage struct {
					TotalUsage uint64 `json:"total_usage"`
				} `json:"cpu_usage"`
				SystemCPUUsage uint64 `json:"system_cpu_usage"`
			}{
				CPUUsage: struct {
					TotalUsage uint64 `json:"total_usage"`
				}{TotalUsage: 400},
				SystemCPUUsage: 800,
			},
		},
	}
	rCpus0 := New(WithClient(mockCpus0))
	m2, err := rCpus0.Metrics(ctx, ref)
	if err != nil {
		t.Fatalf("Metrics unexpected error: %v", err)
	}
	if m2.CPUMillicores != 500 { // (100 / 200) * 1 * 1000 = 500
		t.Errorf("CPUMillicores = %d, want 500", m2.CPUMillicores)
	}
}

func TestDockerRuntime_WatchAvailability(t *testing.T) {
	ctx := context.Background()
	ref := ports.ServerRef{Name: "c1"}

	// Timeout case
	mockNotReady := &mockClient{
		inspect: ContainerInspect{State: ContainerState{Running: false}},
	}
	r := New(WithClient(mockNotReady))
	err := r.WatchAvailability(ctx, ref, 10*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}

	// Ready case
	mockReady := &mockClient{
		inspect: ContainerInspect{State: ContainerState{Running: true}},
	}
	rReady := New(WithClient(mockReady))
	err = rReady.WatchAvailability(ctx, ref, 2500*time.Millisecond)
	if err != nil {
		t.Fatalf("expected nil error on ready container, got %v", err)
	}
}

