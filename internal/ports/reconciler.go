package ports

import (
	"context"
)

// Reconciler makes running reality match the desired state persisted in a StateStore.
//
// In GitOps, reality matches desired state asynchronously: agrelha commits to git,
// and an external controller (ArgoCD) converges the cluster.
// In Docker/Compose, convergence is synchronous and immediate (`compose up`).
type Reconciler interface {
	// Converge reconciles the server's reality with its desired state.
	Converge(ctx context.Context, ref ServerRef) error
	// Async reports whether convergence is external/asynchronous (e.g. ArgoCD)
	// or immediate/synchronous (e.g. local Docker compose).
	Async() bool
}
