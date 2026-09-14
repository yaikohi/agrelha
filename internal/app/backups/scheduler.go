// Package backups holds application services for scheduling instance backups
// and retention pruning.
package backups

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

// JobRunner creates backup jobs in the underlying infrastructure.
type JobRunner interface {
	CreateBackupJob(ctx context.Context, jobName, archiveName, sourcePVC, backupPVC string) error
}

// InstanceLister lists instances.
type InstanceLister interface {
	ListInstances(ctx context.Context) ([]domain.Instance, error)
}

// Option configures a BackupScheduler.
type Option func(*BackupScheduler)

// BackupScheduler snapshots every running Minecraft instance once a day and
// prunes old archives.
type BackupScheduler struct {
	instances      InstanceLister
	jobRunner      JobRunner
	cmdExec        func(ctx context.Context, inst domain.Instance, cmd string) error
	pruner         func(slug string, num, keep int) error
	audit          ports.AuditRecorder
	event          ports.EventRecorder
	namespace      string
	backupsPVC     string
	keep           int
	tickerInterval time.Duration
	timeNow        func(t time.Time) time.Time
	wg             *sync.WaitGroup
}

// New creates a new BackupScheduler with pure interfaces and optional configuration.
func New(instances InstanceLister, runner JobRunner, opts ...Option) *BackupScheduler {
	s := &BackupScheduler{
		instances:      instances,
		jobRunner:      runner,
		namespace:      "minecraft-modded",
		backupsPVC:     "minecraft-modded-backups",
		keep:           5,
		tickerInterval: 15 * time.Minute,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithNamespace sets the kubernetes namespace where the server and PVCs reside.
func WithNamespace(ns string) Option {
	return func(s *BackupScheduler) {
		if ns != "" {
			s.namespace = ns
		}
	}
}

// WithBackupsPVC sets the PVC name where backups are stored.
func WithBackupsPVC(pvc string) Option {
	return func(s *BackupScheduler) {
		if pvc != "" {
			s.backupsPVC = pvc
		}
	}
}

// WithKeep sets how many daily backups to retain.
func WithKeep(keep int) Option {
	return func(s *BackupScheduler) {
		if keep > 0 {
			s.keep = keep
		}
	}
}

// WithCommandExecutor configures a function to run console commands on the instance before/after backup.
func WithCommandExecutor(fn func(ctx context.Context, inst domain.Instance, cmd string) error) Option {
	return func(s *BackupScheduler) {
		s.cmdExec = fn
	}
}

// WithPruner configures an archive pruner function.
func WithPruner(fn func(slug string, num, keep int) error) Option {
	return func(s *BackupScheduler) {
		s.pruner = fn
	}
}

// WithAudit configures the audit recorder.
func WithAudit(a ports.AuditRecorder) Option {
	return func(s *BackupScheduler) {
		s.audit = a
	}
}

// WithEvent configures the event recorder.
func WithEvent(e ports.EventRecorder) Option {
	return func(s *BackupScheduler) {
		s.event = e
	}
}

// Namespace returns the configured namespace.
func (s *BackupScheduler) Namespace() string {
	if s == nil || s.namespace == "" {
		return "minecraft-modded"
	}
	return s.namespace
}

// BackupsPVC returns the configured backups PVC name.
func (s *BackupScheduler) BackupsPVC() string {
	if s == nil || s.backupsPVC == "" {
		return "minecraft-modded-backups"
	}
	return s.backupsPVC
}

// Keep returns the configured retention count.
func (s *BackupScheduler) Keep() int {
	if s == nil || s.keep <= 0 {
		return 5
	}
	return s.keep
}

func defaultTimeNow(t time.Time) time.Time {
	return t.UTC()
}

// Start runs the daily pass at 04:00 UTC, once per calendar day.
func (s *BackupScheduler) Start(ctx context.Context) {
	if s == nil {
		return
	}
	interval := s.tickerInterval
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	timeFn := s.timeNow
	if timeFn == nil {
		timeFn = defaultTimeNow
	}

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		if s.wg != nil {
			defer s.wg.Done()
		}
		var lastDailyDate string

		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				utcNow := timeFn(now)
				currentDate := utcNow.Format("2006-01-02")
				if utcNow.Hour() == 4 && lastDailyDate != currentDate {
					lastDailyDate = currentDate
					s.RunDaily(ctx)
				}
			}
		}
	}()
}

// RunDaily backs up every running instance.
func (s *BackupScheduler) RunDaily(ctx context.Context) {
	if s == nil || s.instances == nil || s.jobRunner == nil {
		return
	}

	instances, err := s.instances.ListInstances(ctx)
	if err != nil {
		slog.Error("scheduler: failed to list minecraft instances", "err", err)
		return
	}

	for _, inst := range instances {
		if inst.State != domain.StateRunning {
			continue
		}
		s.backupOne(ctx, inst)
	}
}

// backupOne is a separate function on purpose: the previous version deferred
// "/save-on" inside a range loop, so with N running instances every server
// stayed in save-off until the whole run finished — and stayed that way
// permanently if the process died mid-run. A per-instance function makes the
// defer fire per instance, which is what was intended.
func (s *BackupScheduler) backupOne(ctx context.Context, inst domain.Instance) {
	slog.Info("scheduler: starting daily backup", "instance", inst.Name, "num", inst.Number, "game", inst.GameID)

	if s.cmdExec != nil && (inst.GameID == "" || inst.GameID == domain.GameMinecraft) {
		_ = s.cmdExec(ctx, inst, "/save-off")
		_ = s.cmdExec(ctx, inst, "/save-all flush")
		defer func() { _ = s.cmdExec(ctx, inst, "/save-on") }()
	}

	prefix := "mc"
	if inst.GameID == domain.GameValheim {
		prefix = "valheim"
	}

	jobName := fmt.Sprintf("%s-backup-%s-%02d-daily-%d", prefix, inst.Slug, inst.Number, time.Now().Unix())
	archiveName := domain.FormatGameBackupFileName(inst.GameID, inst.Slug, inst.Number, "daily")

	if err := s.jobRunner.CreateBackupJob(ctx, jobName, archiveName, inst.PVCName(), s.backupsPVC); err != nil {
		slog.Error("scheduler: failed to create daily backup job", "instance", inst.Name, "err", err)
		return
	}

	if s.pruner != nil {
		_ = s.pruner(inst.Slug, inst.Number, s.keep)
	}
	if s.audit != nil {
		_ = s.audit.RecordAudit("system", prefix+"-backup-daily", fmt.Sprintf("Daily backup created: %s", archiveName))
	}
	if s.event != nil {
		_ = s.event.RecordEvent(prefix+"-backup-daily", fmt.Sprintf("World #%02d %s", inst.Number, inst.Name))
	}
}
