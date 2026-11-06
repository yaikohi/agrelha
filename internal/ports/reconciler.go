package ports

import (
	"context"
)

// Reconciler makes running reality match the desired state persisted in a StateStore.
//
// In GitOps, reality matches desired state asynchronously: agrelha commits to git,
// and an external controller (ArgoCD) converges the cluster.
// In Docker/Compose, convergence is synchronous and immediate (`compose up`).
type Materializer interface {
	Materialize(ctx context.Context, ref ServerRef, files map[string][]byte) error
}

type Reconciler interface {
	// Converge reconciles the server's reality with its desired state.
	Converge(ctx context.Context, ref ServerRef) error
	// Async reports whether convergence is external/asynchronous (e.g. ArgoCD)
	// or immediate/synchronous (e.g. local Docker compose).
	Async() bool
}

// Destroyer tears down everything a Converge created. It is optional: a GitOps
// reconciler does not implement it, because deleting the manifests from the
// state store is itself the instruction for the cluster controller to prune.
// A Docker host has no such controller, so somebody has to do it explicitly.
type Destroyer interface {
	Destroy(ctx context.Context, ref ServerRef) error
}
