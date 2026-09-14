package backups

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"agrelha/internal/domain"
)

type fakeJobRunner struct {
	mu   sync.Mutex
	jobs []string
	err  error
}

func (f *fakeJobRunner) CreateBackupJob(ctx context.Context, jobName, archiveName, sourcePVC, backupPVC string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.jobs = append(f.jobs, jobName)
	return nil
}

func (f *fakeJobRunner) getJobs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.jobs))
	copy(out, f.jobs)
	return out
}

type fakeInstanceLister struct {
	instances []domain.Instance
	err       error
}

func (f *fakeInstanceLister) ListInstances(ctx context.Context) ([]domain.Instance, error) {
	if f.err != nil {
		return nil, f.err
	}
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

func TestSchedulerNilAndZeroGetters(t *testing.T) {
	var nilS *BackupScheduler
	if nilS.Namespace() != "minecraft-modded" {
		t.Errorf("expected default namespace for nil scheduler")
	}
	if nilS.BackupsPVC() != "minecraft-modded-backups" {
		t.Errorf("expected default backupsPVC for nil scheduler")
	}
	if nilS.Keep() != 5 {
		t.Errorf("expected default keep for nil scheduler")
	}

	emptyS := &BackupScheduler{}
	if emptyS.Namespace() != "minecraft-modded" {
		t.Errorf("expected default namespace for empty scheduler")
	}
	if emptyS.BackupsPVC() != "minecraft-modded-backups" {
		t.Errorf("expected default backupsPVC for empty scheduler")
	}
	if emptyS.Keep() != 5 {
		t.Errorf("expected default keep for empty scheduler")
	}

	// Empty string/zero options do not override defaults
	sZero := New(nil, nil, WithNamespace(""), WithBackupsPVC(""), WithKeep(0))
	if sZero.Namespace() != "minecraft-modded" || sZero.BackupsPVC() != "minecraft-modded-backups" || sZero.Keep() != 5 {
		t.Errorf("expected defaults preserved when empty options passed")
	}
}

func TestRunDailyListError(t *testing.T) {
	lister := &fakeInstanceLister{err: fmt.Errorf("list fail")}
	runner := &fakeJobRunner{}
	s := New(lister, runner)
	s.RunDaily(context.Background())
	if len(runner.jobs) != 0 {
		t.Errorf("expected 0 jobs created when listing fails")
	}
}

func TestRunDailyWithCommandExecPrunerAndValheim(t *testing.T) {
	lister := &fakeInstanceLister{
		instances: []domain.Instance{
			{
				Name:   "ValheimWorld",
				Slug:   "valheim-world",
				Number: 1,
				GameID: domain.GameValheim,
				State:  domain.StateRunning,
			},
			{
				Name:   "MCWorld",
				Slug:   "mc-world",
				Number: 2,
				GameID: domain.GameMinecraft,
				State:  domain.StateRunning,
			},
		},
	}
	runner := &fakeJobRunner{}
	rec := &fakeRecorder{}

	var mu sync.Mutex
	var executedCmds []string
	cmdExec := func(ctx context.Context, inst domain.Instance, cmd string) error {
		mu.Lock()
		defer mu.Unlock()
		executedCmds = append(executedCmds, cmd)
		return nil
	}

	var prunedSlugs []string
	pruner := func(slug string, num, keep int) error {
		mu.Lock()
		defer mu.Unlock()
		prunedSlugs = append(prunedSlugs, slug)
		return nil
	}

	s := New(lister, runner,
		WithCommandExecutor(cmdExec),
		WithPruner(pruner),
		WithAudit(rec),
		WithEvent(rec),
	)

	s.RunDaily(context.Background())

	if len(runner.jobs) != 2 {
		t.Fatalf("jobs created = %d, want 2", len(runner.jobs))
	}
	// CmdExec should only be called for Minecraft instance (3 commands: save-off, save-all, save-on)
	if len(executedCmds) != 3 {
		t.Errorf("expected 3 commands for MC instance, got %v", executedCmds)
	}
	if len(prunedSlugs) != 2 {
		t.Errorf("expected 2 pruned slugs, got %v", prunedSlugs)
	}
	if len(rec.audits) != 2 || len(rec.events) != 2 {
		t.Errorf("expected 2 audits and 2 events, got %d audits, %d events", len(rec.audits), len(rec.events))
	}
}

func TestRunDailyJobRunnerError(t *testing.T) {
	lister := &fakeInstanceLister{
		instances: []domain.Instance{
			{
				Name:   "MCWorld",
				Slug:   "mc-world",
				Number: 1,
				GameID: domain.GameMinecraft,
				State:  domain.StateRunning,
			},
		},
	}
	runner := &fakeJobRunner{err: fmt.Errorf("create job failed")}
	rec := &fakeRecorder{}
	s := New(lister, runner, WithAudit(rec), WithEvent(rec))

	s.RunDaily(context.Background())

	if len(rec.audits) != 0 || len(rec.events) != 0 {
		t.Errorf("expected no audit or events recorded when job creation fails")
	}
}

func TestStartHermetic(t *testing.T) {
	runner := &fakeJobRunner{}
	lister := &fakeInstanceLister{
		instances: []domain.Instance{
			{
				Name:   "MCWorld",
				Slug:   "mc-world",
				Number: 1,
				GameID: domain.GameMinecraft,
				State:  domain.StateRunning,
			},
		},
	}

	var wg sync.WaitGroup
	wg.Add(1)

	s := New(lister, runner)
	s.tickerInterval = 5 * time.Millisecond
	s.timeNow = func(t time.Time) time.Time {
		return time.Date(2026, 1, 1, 4, 0, 0, 0, time.UTC)
	}
	s.wg = &wg

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	s.Start(ctx)
	<-ctx.Done()
	wg.Wait()

	if len(runner.getJobs()) == 0 {
		t.Errorf("expected jobs to be created during Start daily pass")
	}
}

func TestStartZeroOptionsAndNil(t *testing.T) {
	var nilS *BackupScheduler
	nilS.Start(context.Background())

	cancelledCtx, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	zeroS := &BackupScheduler{}
	zeroS.Start(cancelledCtx)

	var wg sync.WaitGroup
	wg.Add(1)
	emptyS := &BackupScheduler{
		tickerInterval: 5 * time.Millisecond,
		wg:             &wg,
	}
	ctxEmpty, cancelEmpty := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancelEmpty()
	emptyS.Start(ctxEmpty)
	<-ctxEmpty.Done()
	wg.Wait()
}
