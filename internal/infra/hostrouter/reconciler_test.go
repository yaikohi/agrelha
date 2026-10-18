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
