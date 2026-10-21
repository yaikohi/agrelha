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
	"os"
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

func TestPreConvergeRunsBetweenMaterializeAndConverge(t *testing.T) {
	spy := &recordingReconciler{}
	var order []string
	mgr, ctx := newConvergeManager(t,
		WithReconciler(spy),
		WithPreConverge(func(context.Context, domain.Instance) error {
			order = append(order, "preconverge")
			return nil
		}),
	)

	if _, err := mgr.CreateInstance(ctx, mcWorld("Fluxweave", "fogo"), domain.ModList{}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	if len(spy.order) < 2 || spy.order[0] != "materialize" || spy.order[1] != "converge" {
		t.Fatalf("reconciler order = %v", spy.order)
	}
	if len(order) != 1 {
		t.Fatalf("pre-converge ran %d times, want 1", len(order))
	}
}

func TestAFailingPreConvergeStopsTheConverge(t *testing.T) {
	spy := &recordingReconciler{}
	mgr, ctx := newConvergeManager(t,
		WithReconciler(spy),
		WithPreConverge(func(context.Context, domain.Instance) error {
			return errors.New("merge failed")
		}),
	)

	if _, err := mgr.CreateInstance(ctx, mcWorld("Fluxweave", "fogo"), domain.ModList{}); err == nil {
		t.Fatal("a failed pre-converge must surface, not start a world with unmerged configs")
	}
	for _, step := range spy.order {
		if step == "converge" {
			t.Error("converge ran despite the pre-converge failing")
		}
	}
}

func TestNewWorldsLandOnTheConfiguredDefaultHost(t *testing.T) {
	spy := &recordingReconciler{}
	mgr, ctx := newConvergeManager(t, WithReconciler(spy), WithDefaultHost("fogo"))

	created, err := mgr.CreateInstance(ctx, mcWorld("Fluxweave", ""), domain.ModList{})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if created.Host != domain.Host("fogo") {
		t.Errorf("Host = %q, want fogo; without this nothing can ever create a world off the cluster", created.Host)
	}
}

func TestAnExplicitHostBeatsTheDefault(t *testing.T) {
	mgr, ctx := newConvergeManager(t, WithReconciler(&recordingReconciler{}), WithDefaultHost("fogo"))

	created, err := mgr.CreateInstance(ctx, mcWorld("Fluxweave", domain.HostCluster), domain.ModList{})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if !created.Host.IsCluster() {
		t.Errorf("Host = %q; an explicitly declared Host must win over the default", created.Host)
	}
}

func TestWithNoDefaultHostWorldsStayOnTheCluster(t *testing.T) {
	mgr, ctx := newConvergeManager(t, WithReconciler(&recordingReconciler{}))

	created, err := mgr.CreateInstance(ctx, mcWorld("Fluxweave", ""), domain.ModList{})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if !created.Host.IsCluster() {
		t.Errorf("Host = %q, want cluster when DEFAULT_HOST is unset", created.Host)
	}
}

func TestStartInstanceConvergesWhenContainerMissing(t *testing.T) {
	spy := &recordingReconciler{}
	rt := &mockRuntimePort{err: os.ErrNotExist}
	st, err := store.Open(filepath.Join(t.TempDir(), "converge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	mgr := NewInstanceManager(
		store.NewInstanceRepo(st), newMockStateStore(), rt, 64, 4, 4,
		"manifests/minecraft-modded", "192.168.20.224",
		manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded"), "minecraft-modded",
		WithReconciler(spy),
	)
	ctx := context.Background()

	created, err := mgr.CreateInstance(ctx, mcWorld("Fogueira", "fogo"), domain.ModList{})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	spy.order = nil

	if err := mgr.StartInstance(ctx, created.Number); err != nil {
		t.Fatalf("StartInstance: %v", err)
	}
	if len(spy.order) == 0 || spy.order[len(spy.order)-1] != "converge" {
		t.Errorf("expected converge, got order %v", spy.order)
	}
}

