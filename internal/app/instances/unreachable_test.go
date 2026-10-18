package instances

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/infra/manifests"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
	"io"
)

type downRuntime struct{ err error }

func (d *downRuntime) Start(context.Context, ports.ServerRef) error   { return d.err }
func (d *downRuntime) Stop(context.Context, ports.ServerRef) error    { return d.err }
func (d *downRuntime) Restart(context.Context, ports.ServerRef) error { return d.err }
func (d *downRuntime) Status(context.Context, ports.ServerRef) (ports.Status, error) {
	return ports.Status{}, d.err
}
func (d *downRuntime) Metrics(context.Context, ports.ServerRef) (ports.Metrics, error) {
	return ports.Metrics{}, d.err
}
func (d *downRuntime) Logs(context.Context, ports.ServerRef, ports.LogOptions) (io.ReadCloser, error) {
	return nil, d.err
}
func (d *downRuntime) WatchAvailability(context.Context, ports.ServerRef, time.Duration) error {
	return d.err
}

func managerWithRuntime(t *testing.T, rt ports.Runtime) (*InstanceManager, context.Context) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "unreachable.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	mgr := NewInstanceManager(
		store.NewInstanceRepo(st), newMockStateStore(), rt, 64, 4, 4,
		"manifests/minecraft-modded", "192.168.20.224",
		manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded"), "minecraft-modded",
		WithModsReader(func(context.Context, int) ([]string, error) { return nil, nil }),
	)
	return mgr, context.Background()
}

func seedRunning(t *testing.T, mgr *InstanceManager, ctx context.Context) {
	t.Helper()
	inst, err := mgr.CreateInstance(ctx, domain.Instance{
		Name: "Fluxweave", Source: domain.SourceModlist, Tier: domain.TierMedium,
		Minecraft: &domain.MinecraftConfig{Loader: domain.LoaderNeoForge, MCVersion: "1.21.1"},
	}, domain.ModList{})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	inst.State = domain.StateRunning
	if err := mgr.repo.Upsert(*inst); err != nil {
		t.Fatalf("seed running: %v", err)
	}
}

func TestUnreachableHostIsFlaggedNotSilentlyStale(t *testing.T) {
	mgr, ctx := managerWithRuntime(t, &downRuntime{err: errors.New("dial tcp 192.168.20.11:6443: no route to host")})
	seedRunning(t, mgr, ctx)

	list, err := mgr.ListInstances(ctx)
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 instance, got %d", len(list))
	}
	if !list[0].HostUnreachable {
		t.Error("a world whose Host cannot be reached must be flagged, or the UI shows a stale 'running' as if it were joinable")
	}
}

func TestReachableHostIsNotFlagged(t *testing.T) {
	mgr, ctx := managerWithRuntime(t, &upRuntime{})
	seedRunning(t, mgr, ctx)

	list, err := mgr.ListInstances(ctx)
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if list[0].HostUnreachable {
		t.Error("a reachable Host must never be reported as unreachable")
	}
}

func TestUnreachableHostDoesNotOverwriteStoredState(t *testing.T) {
	mgr, ctx := managerWithRuntime(t, &downRuntime{err: errors.New("no route to host")})
	seedRunning(t, mgr, ctx)

	if _, err := mgr.ListInstances(ctx); err != nil {
		t.Fatalf("ListInstances: %v", err)
	}

	stored, err := mgr.repo.Get(1)
	if err != nil || stored == nil {
		t.Fatalf("get: %v", err)
	}
	if stored.State != domain.StateRunning {
		t.Errorf("stored state = %q; an unreachable Host must not rewrite desired state, only decline to confirm it", stored.State)
	}
	if stored.HostUnreachable {
		t.Error("HostUnreachable is a live observation and must never be persisted")
	}
}

type upRuntime struct{}

func (upRuntime) Start(context.Context, ports.ServerRef) error   { return nil }
func (upRuntime) Stop(context.Context, ports.ServerRef) error    { return nil }
func (upRuntime) Restart(context.Context, ports.ServerRef) error { return nil }
func (upRuntime) Status(context.Context, ports.ServerRef) (ports.Status, error) {
	return ports.Status{Lifecycle: ports.LifecycleRunning, Available: true}, nil
}
func (upRuntime) Metrics(context.Context, ports.ServerRef) (ports.Metrics, error) {
	return ports.Metrics{}, nil
}
func (upRuntime) Logs(context.Context, ports.ServerRef, ports.LogOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (upRuntime) WatchAvailability(context.Context, ports.ServerRef, time.Duration) error {
	return nil
}
