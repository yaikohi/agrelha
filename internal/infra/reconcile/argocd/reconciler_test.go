package argocd

import (
	"context"
	"testing"

	"agrelha/internal/ports"
)

func TestArgoCDReconciler(t *testing.T) {
	ctx := context.Background()
	r := New()

	if r.Async() != true {
		t.Errorf("Async: expected true for ArgoCD, got false")
	}
	if err := r.Converge(ctx, ports.ServerRef{Name: "test"}); err != nil {
		t.Errorf("Converge: expected nil for GitOps, got %v", err)
	}
}
