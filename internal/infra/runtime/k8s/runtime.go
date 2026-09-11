// Package k8s adapts the Kubernetes client to the ports.Runtime interface.
// It is the only place that knows a game server is a Deployment.
package k8s

import (
	"context"
	"io"
	"log/slog"
	"time"

	k8sclient "agrelha/internal/infra/kube"
	"agrelha/internal/ports"
)

// Runtime implements ports.Runtime on top of the existing Kubernetes client.
type Runtime struct {
	c *k8sclient.Client
}

func New(c *k8sclient.Client) *Runtime { return &Runtime{c: c} }

var _ ports.Runtime = (*Runtime)(nil)

// Start and Stop translate the neutral verbs into replica counts. "Scale to 1"
// is the Kubernetes way of saying "start"; the domain never learns that.
func (r *Runtime) Start(ctx context.Context, ref ports.ServerRef) error {
	if r == nil || r.c == nil {
		return ports.ErrNotImplemented
	}
	return r.c.ScaleDeployment(ctx, ref.Name, 1)
}

func (r *Runtime) Stop(ctx context.Context, ref ports.ServerRef) error {
	if r == nil || r.c == nil {
		return ports.ErrNotImplemented
	}
	return r.c.ScaleDeployment(ctx, ref.Name, 0)
}

func (r *Runtime) Restart(ctx context.Context, ref ports.ServerRef) error {
	if r == nil || r.c == nil {
		return ports.ErrNotImplemented
	}
	return r.c.RestartDeployment(ctx, ref.Name)
}

// Status derives Lifecycle from desired replicas and Available from pod
// readiness — never from the pod Phase, whose capital-R "Running" is a
// different concept from Lifecycle's lowercase "running".
func (r *Runtime) Status(ctx context.Context, ref ports.ServerRef) (ports.Status, error) {
	if r == nil || r.c == nil {
		return ports.Status{Lifecycle: ports.LifecycleUnknown}, ports.ErrNotImplemented
	}

	desired, _, err := r.c.DeploymentReplicas(ctx, ref.Name)
	if err != nil {
		return ports.Status{Lifecycle: ports.LifecycleUnknown}, err
	}

	st := ports.Status{Lifecycle: ports.LifecycleStopped}
	if desired > 0 {
		st.Lifecycle = ports.LifecycleRunning
	}

	if ps, err := r.c.DeploymentPodStatus(ctx, ref.Name); err == nil {
		st.Available = ps.Ready
		st.StartedAt = ps.StartedAt
		st.Failure = ports.Failure{
			RestartCount:  ps.RestartCount,
			WaitingReason: ps.WaitingReason,
			ExitCode:      ps.LastExitCode,
			Reason:        ps.LastReason,
			FinishedAt:    ps.LastFinishedAt,
			OOMKilled:     ps.LastOOMKilled,
		}
	}
	if ip, err := r.c.ServiceIP(ctx, ref.Name); err != nil {
		slog.Warn("runtime: cannot read service address, falling back to the stored one",
			"service", ref.Name, "err", err)
	} else {
		st.Address = ip
	}
	return st, nil
}

func (r *Runtime) Metrics(ctx context.Context, ref ports.ServerRef) (ports.Metrics, error) {
	if r == nil || r.c == nil {
		return ports.Metrics{}, ports.ErrNotImplemented
	}
	cpu, mem, err := r.c.PodMetrics(ctx)
	if err != nil {
		return ports.Metrics{}, err
	}
	return ports.Metrics{CPUMillicores: cpu, MemoryMiB: mem, Known: true}, nil
}

func (r *Runtime) Logs(ctx context.Context, ref ports.ServerRef, opts ports.LogOptions) (io.ReadCloser, error) {
	if r == nil || r.c == nil {
		return nil, ports.ErrNotImplemented
	}
	tail := opts.Tail
	if tail <= 0 {
		tail = 200
	}
	return r.c.StreamDeploymentLogsQuery(ctx, ref.Name, k8sclient.LogQuery{
		Tail:     tail,
		Follow:   opts.Follow,
		Previous: opts.Previous,
	})
}

func (r *Runtime) WatchAvailability(ctx context.Context, ref ports.ServerRef, timeout time.Duration) error {
	if r == nil || r.c == nil {
		return ports.ErrNotImplemented
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			st, err := r.Status(ctx, ref)
			if err == nil && st.Available {
				return nil
			}
		}
	}
}
