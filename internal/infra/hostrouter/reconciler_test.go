package hostrouter

import (
	"context"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type spyReconciler struct {
	async     bool
	converged []string
}

func (s *spyReconciler) Converge(_ context.Context, ref ports.ServerRef) error {
	s.converged = append(s.converged, ref.Name)
	return nil
}
func (s *spyReconciler) Async() bool { return s.async }

func TestReconcilerRoutesByRefHost(t *testing.T) {
	argo := &spyReconciler{async: true}
	comp := &spyReconciler{async: false}
	r := NewReconciler(map[domain.Host]ports.Reconciler{domain.HostCluster: argo, "fogo": comp})

	if err := r.Converge(context.Background(), ports.ServerRef{Name: "valheim-odin-01"}); err != nil {
		t.Fatalf("converge cluster: %v", err)
	}
	if err := r.Converge(context.Background(), ports.ServerRef{Name: "valheim-fogo-03", Host: "fogo"}); err != nil {
		t.Fatalf("converge fogo: %v", err)
	}

	if len(argo.converged) != 1 || argo.converged[0] != "valheim-odin-01" {
		t.Errorf("argo saw %v", argo.converged)
	}
	if len(comp.converged) != 1 || comp.converged[0] != "valheim-fogo-03" {
		t.Errorf("compose saw %v", comp.converged)
	}
}

func TestAsyncIsFalseWhenAnyHostNeedsAnExplicitConverge(t *testing.T) {
	onlyArgo := NewReconciler(map[domain.Host]ports.Reconciler{domain.HostCluster: &spyReconciler{async: true}})
	if !onlyArgo.Async() {
		t.Error("a cluster-only router must report async, as it does today")
	}

	mixed := NewReconciler(map[domain.Host]ports.Reconciler{
		domain.HostCluster: &spyReconciler{async: true},
		"fogo":             &spyReconciler{async: false},
	})
	if mixed.Async() {
		t.Error("with a sync Host present the router must report sync, or a caller may skip Converge and never start the world")
	}

	if NewReconciler(nil).Async() {
		t.Error("an empty router must not claim convergence happens elsewhere")
	}
}

func TestReconcilerRefusesUnknownHost(t *testing.T) {
	argo := &spyReconciler{async: true}
	r := NewReconciler(map[domain.Host]ports.Reconciler{domain.HostCluster: argo})

	if err := r.Converge(context.Background(), ports.ServerRef{Name: "x", Host: "nowhere"}); err == nil {
		t.Fatal("an unregistered Host must be an error")
	}
	if len(argo.converged) != 0 {
		t.Error("nothing may be converged when the Host is unknown")
	}
}

type spyDestroyingReconciler struct {
	spyReconciler
	destroyed []string
}

func (s *spyDestroyingReconciler) Destroy(_ context.Context, ref ports.ServerRef) error {
	s.destroyed = append(s.destroyed, ref.Name)
	return nil
}

// DeleteInstance reaches the compose reconciler through this router, so a
// router that silently dropped Destroy would leave every deleted world's
// container, network and saves behind exactly as before the teardown existed.
func TestDestroyReachesTheHostsReconciler(t *testing.T) {
	argo := &spyReconciler{async: true}
	comp := &spyDestroyingReconciler{spyReconciler: spyReconciler{async: false}}
	r := NewReconciler(map[domain.Host]ports.Reconciler{domain.HostCluster: argo, "fogo": comp})

	if err := r.Destroy(context.Background(), ports.ServerRef{Name: "valheim-gaha-03", Host: "fogo"}); err != nil {
		t.Fatalf("destroy fogo: %v", err)
	}
	if len(comp.destroyed) != 1 || comp.destroyed[0] != "valheim-gaha-03" {
		t.Errorf("compose reconciler saw %v, want [valheim-gaha-03]", comp.destroyed)
	}
}

// ArgoCD prunes once the manifests leave the state store, so a reconciler that
// cannot destroy is doing the right thing by doing nothing.
func TestDestroyIsANoOpForAReconcilerThatCannot(t *testing.T) {
	r := NewReconciler(map[domain.Host]ports.Reconciler{domain.HostCluster: &spyReconciler{async: true}})
	if err := r.Destroy(context.Background(), ports.ServerRef{Name: "valheim-odin-01"}); err != nil {
		t.Errorf("destroy on a cluster-only router: %v", err)
	}
}

func TestDestroyFailsForAnUnknownHost(t *testing.T) {
	r := NewReconciler(map[domain.Host]ports.Reconciler{domain.HostCluster: &spyReconciler{async: true}})
	if err := r.Destroy(context.Background(), ports.ServerRef{Name: "valheim-gaha-03", Host: "nowhere"}); err == nil {
		t.Error("destroy accepted a host with no reconciler")
	}
}
