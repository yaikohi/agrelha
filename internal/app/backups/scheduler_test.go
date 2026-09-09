package backups

import (
	"context"
	"testing"

	"agrelha/internal/domain"
)

type fakeJobRunner struct {
	jobs []string
}

func (f *fakeJobRunner) CreateBackupJob(ctx context.Context, jobName, archiveName, sourcePVC, backupPVC string) error {
	f.jobs = append(f.jobs, jobName)
	return nil
}

type fakeInstanceLister struct {
	instances []domain.Instance
}

func (f *fakeInstanceLister) ListInstances(ctx context.Context) ([]domain.Instance, error) {
	return f.instances, nil
}

type fakeRecorder struct {
	audits []string
	events []string
}

func (f *fakeRecorder) RecordAudit(actor, action, detail string) error {
	f.audits = append(f.audits, action)
	return nil
}

func (f *fakeRecorder) RecordEvent(kind, detail string) error {
	f.events = append(f.events, kind)
	return nil
}

func TestRunDailyBacksUpRunningInstances(t *testing.T) {
	lister := &fakeInstanceLister{
		instances: []domain.Instance{
			{
				Name:      "ActiveWorld",
				Slug:      "active-world",
				Number:    1,
				MCVersion: "1.21.1",
				Loader:    domain.LoaderNeoForge,
				Tier:      domain.TierMedium,
				State:     domain.StateRunning,
			},
			{
				Name:   "StoppedWorld",
				Slug:   "stopped-world",
				Number: 2,
				State:  domain.StateStopped,
			},
		},
	}
	runner := &fakeJobRunner{}
	rec := &fakeRecorder{}

	s := New(lister, runner,
		WithAudit(rec),
		WithEvent(rec),
	)

	s.RunDaily(context.Background())

	if len(runner.jobs) != 1 {
		t.Fatalf("jobs created = %d, want 1", len(runner.jobs))
	}
	if len(rec.audits) != 1 || rec.audits[0] != "mc-backup-daily" {
		t.Fatalf("expected audit for mc-backup-daily, got %v", rec.audits)
	}
	if len(rec.events) != 1 || rec.events[0] != "mc-backup-daily" {
		t.Fatalf("expected event for mc-backup-daily, got %v", rec.events)
	}
}

// The scheduler must never panic on a partially-wired server; it runs
// unattended in a goroutine where a panic takes the process down.
func TestRunDailyIsSafeWhenUnwired(t *testing.T) {
	(&BackupScheduler{}).RunDaily(context.Background())

	s := New(nil, nil)
	s.RunDaily(context.Background())

	lister := &fakeInstanceLister{}
	s2 := New(lister, nil)
	s2.RunDaily(context.Background())
}

// Namespace, backups PVC and retention defaults and overrides.
func TestSchedulerDefaultsAreOverridable(t *testing.T) {
	s := New(nil, nil)
	if got := s.Namespace(); got != "minecraft-modded" {
		t.Fatalf("namespace default = %q, want minecraft-modded", got)
	}
	if got := s.Keep(); got != 5 {
		t.Fatalf("keep default = %d, want 5", got)
	}

	s2 := New(nil, nil, WithNamespace("custom-ns"), WithKeep(2), WithBackupsPVC("custom-pvc"))
	if got := s2.Namespace(); got != "custom-ns" {
		t.Fatalf("namespace = %q, want custom-ns", got)
	}
	if got := s2.Keep(); got != 2 {
		t.Fatalf("keep = %d, want 2", got)
	}
	if got := s2.BackupsPVC(); got != "custom-pvc" {
		t.Fatalf("backupsPVC = %q, want custom-pvc", got)
	}
}
