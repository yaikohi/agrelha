package instances

import (
	"context"
	"errors"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type spyDestroyer struct {
	spyConverger
	destroyed  []ports.ServerRef
	destroyErr error
}

func (s *spyDestroyer) Destroy(_ context.Context, ref ports.ServerRef) error {
	s.destroyed = append(s.destroyed, ref)
	return s.destroyErr
}

func createdWorld(t *testing.T, mgr *InstanceManager, ctx context.Context, host domain.Host) domain.Instance {
	t.Helper()
	created, err := mgr.CreateInstance(ctx, mcWorld("Fluxweave", host), domain.ModList{})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	return *created
}

// On a Docker host nothing prunes a deleted world, so DeleteInstance owns the
// teardown. Without it the container, its network and its saves outlived every
// record that pointed at them.
func TestDeleteInstanceTearsDownTheRuntimeOnADockerHost(t *testing.T) {
	spy := &spyDestroyer{}
	mgr, ctx := newConvergeManager(t, WithReconciler(spy))
	created := createdWorld(t, mgr, ctx, "fogo")

	if err := mgr.DeleteInstance(ctx, created.Number); err != nil {
		t.Fatalf("DeleteInstance: %v", err)
	}
	if len(spy.destroyed) != 1 {
		t.Fatalf("Destroy called %d times, want 1", len(spy.destroyed))
	}
	if got := spy.destroyed[0].ID; got.Number != created.Number || got.Game != domain.GameMinecraft {
		t.Errorf("Destroy got ref ID %+v, want minecraft #%d", got, created.Number)
	}
	if left, err := mgr.ListInstances(ctx); err != nil || len(left) != 0 {
		t.Errorf("instance survived delete: %v, %v", left, err)
	}
}

// In the cluster, dropping the manifests is the instruction to prune; running
// compose down against a Host that has no Docker would be wrong.
func TestDeleteInstanceLeavesClusterTeardownToTheController(t *testing.T) {
	spy := &spyDestroyer{}
	mgr, ctx := newConvergeManager(t, WithReconciler(spy))
	created := createdWorld(t, mgr, ctx, domain.HostCluster)

	if err := mgr.DeleteInstance(ctx, created.Number); err != nil {
		t.Fatalf("DeleteInstance: %v", err)
	}
	if len(spy.destroyed) != 0 {
		t.Errorf("Destroy ran for a cluster world: %+v", spy.destroyed)
	}
}

// A teardown that failed must abort the delete. Dropping the records anyway is
// exactly how a world becomes an orphan nobody can find again.
func TestDeleteInstanceKeepsTheWorldWhenTeardownFails(t *testing.T) {
	spy := &spyDestroyer{destroyErr: errors.New("docker socket is gone")}
	mgr, ctx := newConvergeManager(t, WithReconciler(spy))
	created := createdWorld(t, mgr, ctx, "fogo")

	err := mgr.DeleteInstance(ctx, created.Number)
	if err == nil {
		t.Fatal("DeleteInstance reported success after teardown failed")
	}
	left, listErr := mgr.ListInstances(ctx)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(left) != 1 {
		t.Fatalf("world was deleted despite a failed teardown, leaving an orphan: %v", left)
	}
}
