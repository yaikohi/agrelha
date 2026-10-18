package instances

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/infra/manifests"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
)

type spyConverger struct {
	refs []ports.ServerRef
	err  error
}

func (s *spyConverger) Converge(_ context.Context, ref ports.ServerRef) error {
	s.refs = append(s.refs, ref)
	return s.err
}

func (s *spyConverger) Async() bool { return false }

func newConvergeManager(t *testing.T, opts ...Option) (*InstanceManager, context.Context) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "converge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	base := []Option{WithModsReader(func(context.Context, int) ([]string, error) { return nil, nil })}
	mgr := NewInstanceManager(
		store.NewInstanceRepo(st), newMockStateStore(), nil, 64, 4, 4,
		"manifests/minecraft-modded", "192.168.20.224",
		manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded"), "minecraft-modded",
		append(base, opts...)...,
	)
	return mgr, context.Background()
}

func mcWorld(name string, host domain.Host) domain.Instance {
	return domain.Instance{
		Name: name, Source: domain.SourceModlist, Tier: domain.TierMedium, Host: host,
		Minecraft: &domain.MinecraftConfig{Loader: domain.LoaderNeoForge, MCVersion: "1.21.1"},
	}
}

func TestNoReconcilerStillWritesManifests(t *testing.T) {
	mgr, ctx := newConvergeManager(t)

	if _, err := mgr.CreateInstance(ctx, mcWorld("Fluxweave", ""), domain.ModList{}); err != nil {
		t.Fatalf("CreateInstance without a reconciler must still work: %v", err)
	}
}

func TestConvergeIsCalledWithTheInstanceHost(t *testing.T) {
	spy := &spyConverger{}
	mgr, ctx := newConvergeManager(t, WithReconciler(spy))

	if _, err := mgr.CreateInstance(ctx, mcWorld("Fluxweave", ""), domain.ModList{}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if len(spy.refs) == 0 {
		t.Fatal("writing manifests must ask the declarative plane to converge")
	}
	if got := spy.refs[0].Host; got != domain.HostCluster {
		t.Errorf("Host = %q, want %q", got, domain.HostCluster)
	}
}

func TestConvergeCarriesANonClusterHost(t *testing.T) {
	spy := &spyConverger{}
	mgr, ctx := newConvergeManager(t, WithReconciler(spy))

	if _, err := mgr.CreateInstance(ctx, mcWorld("Fogueira", "fogo"), domain.ModList{}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if len(spy.refs) == 0 {
		t.Fatal("no converge recorded")
	}
	if got := spy.refs[0].Host; got != domain.Host("fogo") {
		t.Errorf("Host = %q, want fogo; a world on another Host must not converge against the cluster", got)
	}
}

func TestAFailedConvergeIsReported(t *testing.T) {
	spy := &spyConverger{err: errors.New("compose up failed")}
	mgr, ctx := newConvergeManager(t, WithReconciler(spy))

	_, err := mgr.CreateInstance(ctx, mcWorld("Fluxweave", ""), domain.ModList{})
	if err == nil {
		t.Fatal("a convergence failure must surface, not be swallowed")
	}
}

type recordingReconciler struct {
	order []string
}

func (r *recordingReconciler) Materialize(_ context.Context, _ ports.ServerRef, files map[string][]byte) error {
	r.order = append(r.order, "materialize")
	if len(files) == 0 {
		return errors.New("materialize received no files to write")
	}
	return nil
}

func (r *recordingReconciler) Converge(_ context.Context, _ ports.ServerRef) error {
	r.order = append(r.order, "converge")
	return nil
}

func (r *recordingReconciler) Async() bool { return false }

func TestFilesAreMaterializedBeforeConverging(t *testing.T) {
	spy := &recordingReconciler{}
	mgr, ctx := newConvergeManager(t, WithReconciler(spy))

	if _, err := mgr.CreateInstance(ctx, mcWorld("Fluxweave", "fogo"), domain.ModList{}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	if len(spy.order) < 2 || spy.order[0] != "materialize" || spy.order[1] != "converge" {
		t.Errorf("order = %v; converging before the files are on disk would run compose against an empty directory", spy.order)
	}
}
