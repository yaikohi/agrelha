package instances

import (
	"agrelha/internal/domain"
	"agrelha/internal/infra/manifests"
	"context"
	"io"
	"path/filepath"
	"testing"

	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
)

// fakeRuntime is a ports.Runtime with no Kubernetes anywhere in it. If the
// manager can be driven entirely through this, the port is genuinely
// runtime-neutral rather than "Kubernetes with extra steps".
type fakeRuntime struct {
	started, stopped []string
	status           map[string]ports.Status
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{status: map[string]ports.Status{}}
}

func (f *fakeRuntime) Start(_ context.Context, ref ports.ServerRef) error {
	f.started = append(f.started, ref.Name)
	f.status[ref.Name] = ports.Status{Lifecycle: ports.LifecycleRunning, Available: true}
	return nil
}

func (f *fakeRuntime) Stop(_ context.Context, ref ports.ServerRef) error {
	f.stopped = append(f.stopped, ref.Name)
	f.status[ref.Name] = ports.Status{Lifecycle: ports.LifecycleStopped}
	return nil
}

func (f *fakeRuntime) Restart(context.Context, ports.ServerRef) error { return nil }

func (f *fakeRuntime) Status(_ context.Context, ref ports.ServerRef) (ports.Status, error) {
	if st, ok := f.status[ref.Name]; ok {
		return st, nil
	}
	return ports.Status{Lifecycle: ports.LifecycleStopped}, nil
}

func (f *fakeRuntime) Metrics(context.Context, ports.ServerRef) (ports.Metrics, error) {
	return ports.Metrics{}, nil
}

func (f *fakeRuntime) Logs(context.Context, ports.ServerRef, ports.LogOptions) (io.ReadCloser, error) {
	return nil, ports.ErrNotImplemented
}

var _ ports.Runtime = (*fakeRuntime)(nil)

func TestManagerDrivesAnyRuntime(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	rt := newFakeRuntime()
	mgr := NewInstanceManager(
		store.NewInstanceRepo(st), nil, rt, 24, 4, 2, "manifests/minecraft-modded", "", manifests.New("", "minecraft-modded"), "minecraft-modded")
	ctx := context.Background()

	inst, err := mgr.CreateInstance(ctx, domain.Instance{
		Name: "portcheck", Loader: domain.LoaderNeoForge, Source: domain.SourceModlist,
		MCVersion: "1.21.1", Tier: domain.TierSmall,
	}, "jei\n")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := mgr.StartInstance(ctx, inst.Number); err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(rt.started) != 1 || rt.started[0] != inst.DeploymentName() {
		t.Fatalf("runtime was not asked to start the instance: %v", rt.started)
	}

	// The manager must read lifecycle back through the port, not from k8s.
	got, err := mgr.GetInstance(ctx, inst.Number)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.StateRunning {
		t.Fatalf("state = %q, want running (derived from the port's Status)", got.State)
	}

	if err := mgr.StopInstance(ctx, inst.Number); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(rt.stopped) != 1 {
		t.Fatalf("runtime was not asked to stop the instance: %v", rt.stopped)
	}
	if got, _ := mgr.GetInstance(ctx, inst.Number); got.State != domain.StateStopped {
		t.Fatalf("state = %q, want stopped", got.State)
	}
}

// Lifecycle and Availability are separate facts; a server that is "running" but
// not yet reachable is provisioning, not running. This is the distinction that
// once made a healthy Valheim server report Offline.
func TestStateFromStatusSeparatesLifecycleAndAvailability(t *testing.T) {
	cases := []struct {
		name string
		in   ports.Status
		want domain.InstanceState
	}{
		{"reachable", ports.Status{Lifecycle: ports.LifecycleRunning, Available: true}, domain.StateRunning},
		{"asked to run, not reachable yet", ports.Status{Lifecycle: ports.LifecycleRunning}, domain.StateProvisioning},
		{"stopped", ports.Status{Lifecycle: ports.LifecycleStopped}, domain.StateStopped},
		{"unknown runtime", ports.Status{Lifecycle: ports.LifecycleUnknown}, domain.StateProvisioning},
	}
	for _, c := range cases {
		if got := stateFromStatus(c.in); got != c.want {
			t.Errorf("%s: stateFromStatus = %q, want %q", c.name, got, c.want)
		}
	}
}
