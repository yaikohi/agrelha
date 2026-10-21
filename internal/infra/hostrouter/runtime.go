package hostrouter

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type RuntimeRouter struct {
	routes map[domain.Host]ports.Runtime
}

func NewRuntime(routes map[domain.Host]ports.Runtime) *RuntimeRouter {
	normalized := make(map[domain.Host]ports.Runtime, len(routes))
	for h, rt := range routes {
		if rt == nil {
			continue
		}
		normalized[domain.NormalizeHost(string(h))] = rt
	}
	return &RuntimeRouter{routes: normalized}
}

var _ ports.Runtime = (*RuntimeRouter)(nil)

func (r *RuntimeRouter) Hosts() []string {
	out := make([]string, 0, len(r.routes))
	for h := range r.routes {
		out = append(out, string(h))
	}
	sort.Strings(out)
	return out
}

func (r *RuntimeRouter) For(host domain.Host) (ports.Runtime, error) {
	h := domain.NormalizeHost(string(host))
	rt, ok := r.routes[h]
	if !ok {
		return nil, fmt.Errorf("no runtime registered for host %q (known: %s)", h, strings.Join(r.Hosts(), ", "))
	}
	return rt, nil
}

func (r *RuntimeRouter) Start(ctx context.Context, ref ports.ServerRef) error {
	rt, err := r.For(ref.Host)
	if err != nil {
		return err
	}
	return rt.Start(ctx, ref)
}

func (r *RuntimeRouter) Stop(ctx context.Context, ref ports.ServerRef) error {
	rt, err := r.For(ref.Host)
	if err != nil {
		return err
	}
	return rt.Stop(ctx, ref)
}

func (r *RuntimeRouter) Restart(ctx context.Context, ref ports.ServerRef) error {
	rt, err := r.For(ref.Host)
	if err != nil {
		return err
	}
	return rt.Restart(ctx, ref)
}

func (r *RuntimeRouter) Status(ctx context.Context, ref ports.ServerRef) (ports.Status, error) {
	rt, err := r.For(ref.Host)
	if err != nil {
		return ports.Status{}, err
	}
	return rt.Status(ctx, ref)
}

func (r *RuntimeRouter) Metrics(ctx context.Context, ref ports.ServerRef) (ports.Metrics, error) {
	rt, err := r.For(ref.Host)
	if err != nil {
		return ports.Metrics{}, err
	}
	return rt.Metrics(ctx, ref)
}

func (r *RuntimeRouter) Logs(ctx context.Context, ref ports.ServerRef, opts ports.LogOptions) (io.ReadCloser, error) {
	rt, err := r.For(ref.Host)
	if err != nil {
		return nil, err
	}
	return rt.Logs(ctx, ref, opts)
}

func (r *RuntimeRouter) WatchAvailability(ctx context.Context, ref ports.ServerRef, timeout time.Duration) error {
	rt, err := r.For(ref.Host)
	if err != nil {
		return err
	}
	return rt.WatchAvailability(ctx, ref, timeout)
}
