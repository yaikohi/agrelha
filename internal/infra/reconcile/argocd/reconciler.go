// Package argocd implements ports.Reconciler for GitOps with ArgoCD.
// Under GitOps, desired state is committed to git and reconciled externally and
// asynchronously by ArgoCD.
package argocd

import (
	"context"

	"agrelha/internal/ports"
)

type Reconciler struct{}

func New() *Reconciler { return &Reconciler{} }

var _ ports.Reconciler = (*Reconciler)(nil)

func (r *Reconciler) Converge(ctx context.Context, ref ports.ServerRef) error {
	// In GitOps, writing state to the StateStore is the action that initiates convergence.
	// Convergence is handled externally by ArgoCD.
	return nil
}

func (r *Reconciler) Async() bool {
	return true
}
