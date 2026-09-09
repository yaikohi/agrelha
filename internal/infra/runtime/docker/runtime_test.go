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
