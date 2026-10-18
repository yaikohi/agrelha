package argocd

import (
	"testing"

	"agrelha/internal/ports"
)

func TestArgoCDDoesNotMaterialize(t *testing.T) {
	var r ports.Reconciler = New()
	if _, ok := r.(ports.Materializer); ok {
		t.Error("GitOps delivery is the commit; writing files locally would create a second source of truth beside the repo")
	}
}
