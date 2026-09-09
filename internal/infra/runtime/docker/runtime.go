// Package docker adapts the Docker engine to ports.Runtime.
package docker

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"agrelha/internal/ports"
)

// Runtime is the Docker engine implementation of ports.Runtime.
type Runtime struct {
	client Client
}

// Option configures Runtime.
type Option func(*Runtime)

// WithClient configures a custom Docker client.
func WithClient(c Client) Option {
	return func(r *Runtime) {
		r.client = c
	}
}

// New creates a new Docker Runtime adapter.
func New(opts ...Option) *Runtime {
	r := &Runtime{}
	for _, opt := range opts {
		opt(r)
	}
	if r.client == nil {
		r.client = NewSocketClient("")
	}
	return r
}

var _ ports.Runtime = (*Runtime)(nil)

func (r *Runtime) Start(ctx context.Context, ref ports.ServerRef) error {
	if r == nil || r.client == nil {
		return ports.ErrNotImplemented
	}
	return r.client.ContainerStart(ctx, ref.Name)
}

func (r *Runtime) Stop(ctx context.Context, ref ports.ServerRef) error {
	if r == nil || r.client == nil {
		return ports.ErrNotImplemented
	}
	return r.client.ContainerStop(ctx, ref.Name)
}

func (r *Runtime) Restart(ctx context.Context, ref ports.ServerRef) error {
	if r == nil || r.client == nil {
		return ports.ErrNotImplemented
	}
	return r.client.ContainerRestart(ctx, ref.Name)
}

func (r *Runtime) Status(ctx context.Context, ref ports.ServerRef) (ports.Status, error) {
	if r == nil || r.client == nil {
		return ports.Status{Lifecycle: ports.LifecycleUnknown}, ports.ErrNotImplemented
	}

	inspect, err := r.client.ContainerInspect(ctx, ref.Name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ports.Status{Lifecycle: ports.LifecycleStopped, Available: false}, nil
		}
		return ports.Status{Lifecycle: ports.LifecycleUnknown}, err
	}

	st := ports.Status{
		Lifecycle: ports.LifecycleStopped,
		Available: false,
	}

	if inspect.State.Running {
		st.Lifecycle = ports.LifecycleRunning
	}

	if inspect.State.Health != nil && inspect.State.Health.Status != "" && inspect.State.Health.Status != "none" {
		st.Available = inspect.State.Health.Status == "healthy"
	} else {
		st.Available = inspect.State.Running
	}

	if inspect.State.StartedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, inspect.State.StartedAt); err == nil {
			st.StartedAt = t
		}
	}

	return st, nil
}

func (r *Runtime) Metrics(ctx context.Context, ref ports.ServerRef) (ports.Metrics, error) {
	if r == nil || r.client == nil {
		return ports.Metrics{}, ports.ErrNotImplemented
	}

	stats, err := r.client.ContainerStats(ctx, ref.Name)
	if err != nil {
		return ports.Metrics{}, err
	}

	var memMiB int64
	if stats.MemoryStats.Usage > 0 {
		memMiB = stats.MemoryStats.Usage / (1024 * 1024)
	}

	var millicores int64
	cpuDelta := stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage
	sysDelta := stats.CPUStats.SystemCPUUsage - stats.PreCPUStats.SystemCPUUsage
	if sysDelta > 0 && cpuDelta > 0 {
		cpus := stats.CPUStats.OnlineCPUs
		if cpus == 0 {
			cpus = 1
		}
		millicores = int64((float64(cpuDelta) / float64(sysDelta)) * float64(cpus) * 1000.0)
	}

	return ports.Metrics{
		CPUMillicores: millicores,
		MemoryMiB:     memMiB,
		Known:         true,
	}, nil
}

func (r *Runtime) Logs(ctx context.Context, ref ports.ServerRef, opts ports.LogOptions) (io.ReadCloser, error) {
	if r == nil || r.client == nil {
		return nil, ports.ErrNotImplemented
	}
	return r.client.ContainerLogs(ctx, ref.Name, opts)
}
