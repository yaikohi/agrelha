package hostrouter

import (
	"context"
	"fmt"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type ReconcilerRouter struct {
	hostMap[ports.Reconciler]
}

func NewReconciler(routes map[domain.Host]ports.Reconciler) *ReconcilerRouter {
	return &ReconcilerRouter{hostMap: newHostMap("reconciler", routes)}
}

var _ ports.Reconciler = (*ReconcilerRouter)(nil)

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
	if ok {
		return mat.Materialize(ctx, ref, files)
	}
	if rc.Async() {
		return nil
	}
	return fmt.Errorf("host %q reconciler does not implement ports.Materializer: %w", ref.Host, ports.ErrNotImplemented)
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
