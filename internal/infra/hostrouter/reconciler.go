package hostrouter

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type ReconcilerRouter struct {
	routes map[domain.Host]ports.Reconciler
}

func NewReconciler(routes map[domain.Host]ports.Reconciler) *ReconcilerRouter {
	normalized := make(map[domain.Host]ports.Reconciler, len(routes))
	for h, rc := range routes {
		if rc == nil {
			continue
		}
		normalized[domain.NormalizeHost(string(h))] = rc
	}
	return &ReconcilerRouter{routes: normalized}
}

var _ ports.Reconciler = (*ReconcilerRouter)(nil)

func (r *ReconcilerRouter) Hosts() []string {
	out := make([]string, 0, len(r.routes))
	for h := range r.routes {
		out = append(out, string(h))
	}
	sort.Strings(out)
	return out
}

func (r *ReconcilerRouter) For(host domain.Host) (ports.Reconciler, error) {
	h := domain.NormalizeHost(string(host))
	rc, ok := r.routes[h]
	if !ok {
		return nil, fmt.Errorf("no reconciler registered for host %q (known: %s)", h, strings.Join(r.Hosts(), ", "))
	}
	return rc, nil
}

func (r *ReconcilerRouter) Converge(ctx context.Context, ref ports.ServerRef) error {
	rc, err := r.For(ref.Host)
	if err != nil {
		return err
	}
	return rc.Converge(ctx, ref)
}

func (r *ReconcilerRouter) Materialize(ctx context.Context, ref ports.ServerRef, files map[string][]byte) error {
	rc, err := r.For(ref.Host)
	if err != nil {
		return err
	}
	mat, ok := rc.(ports.Materializer)
	if !ok {
		return nil
	}
	return mat.Materialize(ctx, ref, files)
}

var _ ports.Materializer = (*ReconcilerRouter)(nil)

func (r *ReconcilerRouter) Async() bool {
	if len(r.routes) == 0 {
		return false
	}
	for _, rc := range r.routes {
		if !rc.Async() {
			return false
		}
	}
	return true
}
