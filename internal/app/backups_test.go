package app

import (
	"context"
	"path/filepath"
	"testing"

	"k8s.io/client-go/kubernetes/fake"

	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/store"
	"agrelha/internal/minecraft"
	"agrelha/internal/platform/config"
)

func newScheduler(t *testing.T) (*BackupScheduler, *minecraft.InstanceManager, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	kc := k8s.NewWithClientset(fake.NewSimpleClientset(), "minecraft-modded", "mc")
	mgr := minecraft.NewInstanceManager(
		store.NewInstanceRepo(st), nil, nil, 24, 4, 2,
		"manifests/minecraft-modded", "", "", "minecraft-modded",
	)
	return &BackupScheduler{
		Cfg:       &config.Config{},
		Store:     st,
		Instances: mgr,
		K8s:       kc,
		Namespace: "minecraft-modded",
	}, mgr, st
}

func TestRunDailyBacksUpRunningInstances(t *testing.T) {
	s, mgr, st := newScheduler(t)
	defer st.Close()

	if _, err := mgr.CreateInstance(context.Background(), minecraft.Instance{
		Name: "ActiveWorld", MCVersion: "1.21.1",
		Loader: minecraft.LoaderNeoForge, Tier: minecraft.TierMedium,
		State: minecraft.StateRunning,
	}, ""); err != nil {
		t.Fatal(err)
	}

	s.RunDaily(context.Background())

	// The run must be recorded, proving it actually reached the backup path
	// rather than bailing out early.
	hist, err := st.ListHistory(50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range hist {
		if h.Kind == "mc-backup-daily" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a mc-backup-daily audit/event entry")
	}
}

// The scheduler must never panic on a partially-wired server; it runs
// unattended in a goroutine where a panic takes the process down.
func TestRunDailyIsSafeWhenUnwired(t *testing.T) {
	(&BackupScheduler{}).RunDaily(context.Background())

	s, _, st := newScheduler(t)
	defer st.Close()
	s.K8s = nil
	s.RunDaily(context.Background())
}

// Namespace, backups PVC and retention were hardcoded in the old scheduler,
// bypassing the configuration phase 0 introduced.
func TestSchedulerDefaultsAreOverridable(t *testing.T) {
	s := &BackupScheduler{Cfg: &config.Config{MinecraftNamespace: "games"}}
	if got := s.namespace(); got != "games" {
		t.Fatalf("namespace = %q, want games (from config)", got)
	}
	s.Namespace = "explicit"
	if got := s.namespace(); got != "explicit" {
		t.Fatalf("explicit namespace ignored, got %q", got)
	}
	if got := (&BackupScheduler{}).keep(); got != 5 {
		t.Fatalf("keep default = %d, want 5", got)
	}
	if got := (&BackupScheduler{Keep: 2}).keep(); got != 2 {
		t.Fatalf("keep override ignored, got %d", got)
	}
}
