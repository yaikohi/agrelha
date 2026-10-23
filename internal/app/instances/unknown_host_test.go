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

type wrappedUnknownHostRuntime struct {
	mockRuntimePort
}

func (r *wrappedUnknownHostRuntime) Status(ctx context.Context, ref ports.ServerRef) (ports.Status, error) {
	return ports.Status{}, errors.Join(errors.New("no runtime registered for host \"fgoo\""), ports.ErrUnknownHost)
}

func hostErrorManager(t *testing.T) *InstanceManager {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "hosterr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewInstanceManager(
		store.NewInstanceRepo(st), newMockStateStore(), nil, 64, 4, 4,
		"manifests/minecraft-modded", "192.168.20.224",
		manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded"), "minecraft-modded",
		WithModsReader(func(context.Context, int) ([]string, error) { return nil, nil }),
	)
}

func TestAnUnreachableHostKeepsTheLastKnownStateAndFlagsIt(t *testing.T) {
	mgr := hostErrorManager(t)
	ctx := context.Background()
	created, err := mgr.CreateInstance(ctx, mcWorld("Fogueira", "fogo"), domain.ModList{})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if err := mgr.repo.UpdateState(created.Number, domain.StateRunning); err != nil {
		t.Fatal(err)
	}
	mgr.runtime = &mockRuntimePort{err: errors.New("dial unix /var/run/docker.sock: connect: no such file")}

	one, err := mgr.GetInstance(ctx, created.Number)
	if err != nil {
		t.Fatalf("an unreachable Host must not fail the read: %v", err)
	}
	if !one.HostUnreachable {
		t.Error("HostUnreachable was not set for a runtime that could not be dialled")
	}
	if one.HostUnknown {
		t.Error("HostUnknown was set for a Host that is merely unreachable; the two conditions have different remedies")
	}
	if one.State != domain.StateRunning {
		t.Errorf(`an unreachable Host overwrote the last known State (got %q, want %q).

The pill derives "Unknown" from HostUnreachable, so State must stay the last value agrelha
saw. Overwriting it makes every guard that asks "is this world running?" answer no - and an
in-place restore, which refuses to run on a live world, would then be permitted.`, one.State, domain.StateRunning)
	}
}

func TestAnUnknownHostIsAHardErrorOnRead(t *testing.T) {
	mgr := hostErrorManager(t)
	ctx := context.Background()
	created, err := mgr.CreateInstance(ctx, mcWorld("Fogueira", "fogo"), domain.ModList{})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	mgr.runtime = &wrappedUnknownHostRuntime{}

	one, err := mgr.GetInstance(ctx, created.Number)
	if !errors.Is(err, ports.ErrUnknownHost) {
		t.Errorf(`GetInstance returned (%v, %v) for an Instance naming a Host no runtime serves.

Plan 0003 verification 4: "An Instance whose Host is unknown produces a hard error, never a
default Host." Reporting it as HostUnreachable instead tells the operator to wait for a machine
to come back, when the real remedy is to fix the Host in configuration.`, one, err)
	}
}

func TestAnUnknownHostStillListsSoOrphansCanBeSeen(t *testing.T) {
	mgr := hostErrorManager(t)
	ctx := context.Background()
	if _, err := mgr.CreateInstance(ctx, mcWorld("Fogueira", "fogo"), domain.ModList{}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	mgr.runtime = &wrappedUnknownHostRuntime{}

	list, err := mgr.ListInstances(ctx)
	if err != nil {
		t.Fatalf(`ListInstances failed for an Instance with an unknown Host: %v

Decommissioning a Host would then make every world on it invisible, leaving no way to see or
delete the orphans. The list must stay readable; the hard error belongs on GetInstance.`, err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(list))
	}
	if !list[0].HostUnknown {
		t.Error("ListInstances did not flag HostUnknown, so the pill cannot distinguish a misconfigured Host from a sleeping one")
	}
	if list[0].HostUnreachable {
		t.Error("ListInstances flagged HostUnreachable for an unknown Host, which is the conflation this change removes")
	}
}
