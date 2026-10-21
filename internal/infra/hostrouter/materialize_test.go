package hostrouter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/infra/reconcile/argocd"
	"agrelha/internal/infra/reconcile/compose"
	"agrelha/internal/ports"
)

func TestRouterForwardsMaterializeToTheRoutedReconciler(t *testing.T) {
	work := t.TempDir()
	r := NewReconciler(map[domain.Host]ports.Reconciler{
		domain.HostCluster: argocd.New(),
		"fogo":             compose.New(compose.WithWorkDir(work)),
	})

	mat, ok := any(r).(ports.Materializer)
	if !ok {
		t.Fatal("the router must carry Materialize through, or the manager silently skips it and converge runs against a directory nobody created")
	}

	ref := ports.ServerRef{Name: "valheim-odin-01", ID: domain.NewInstanceID(domain.GameValheim, 1)}
	ref.Host = "fogo"
	if err := mat.Materialize(context.Background(), ref, map[string][]byte{
		"docker-compose.yml": []byte("services: {}\n"),
	}); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	written := filepath.Join(work, "valheim", "instance-01", "docker-compose.yml")
	if _, err := os.Stat(written); err != nil {
		t.Errorf("compose file not written to %s: %v", written, err)
	}
}

func TestMaterializeIsANoOpForAHostThatDoesNotNeedIt(t *testing.T) {
	r := NewReconciler(map[domain.Host]ports.Reconciler{domain.HostCluster: argocd.New()})

	err := r.Materialize(context.Background(), ports.ServerRef{Name: "valheim-odin-01"}, map[string][]byte{
		"deployment.yaml": []byte("x"),
	})
	if err != nil {
		t.Errorf("a GitOps Host materializes nothing locally and must not error: %v", err)
	}
}

func TestMaterializeRefusesAnUnknownHost(t *testing.T) {
	r := NewReconciler(map[domain.Host]ports.Reconciler{domain.HostCluster: argocd.New()})

	err := r.Materialize(context.Background(), ports.ServerRef{Name: "x", Host: "nowhere"}, nil)
	if err == nil {
		t.Error("an unregistered Host must fail here too, not silently write nothing")
	}
}
