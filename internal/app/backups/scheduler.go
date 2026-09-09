// Package app holds application services: use cases that orchestrate the domain
// and the ports, and belong to neither. They are not HTTP handlers (nothing here
// touches fiber) and they are not domain rules (they coordinate infrastructure).
//
// This is the layer a JVM codebase would call `application`. See
// docs/modularization-plan.md §7.
package backups

import (
	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"agrelha/internal/infra/backups"
	"agrelha/internal/infra/rcon"
	"context"
	"fmt"
	"log/slog"
	"time"

	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/store"
	"agrelha/internal/platform/config"
)

// BackupScheduler snapshots every running Minecraft instance once a day and
// prunes old archives.
type BackupScheduler struct {
	Cfg        *config.Config
	Store      *store.Store
	Instances  *instances.InstanceManager
	K8s        *k8s.Client
	RconPool   *rcon.Pool
	Namespace  string
	BackupsPVC string
	Keep       int
}

func (s *BackupScheduler) namespace() string {
	if s.Namespace != "" {
		return s.Namespace
	}
	if s.Cfg != nil && s.Cfg.MinecraftNamespace != "" {
		return s.Cfg.MinecraftNamespace
	}
	return "minecraft-modded"
}

func (s *BackupScheduler) backupsPVC() string {
	if s.BackupsPVC != "" {
		return s.BackupsPVC
	}
	return "minecraft-modded-backups"
}

func (s *BackupScheduler) keep() int {
	if s.Keep > 0 {
		return s.Keep
	}
	return 5
}

// Start runs the daily pass at 04:00 UTC, once per calendar day.
func (s *BackupScheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Minute)
	go func() {
		defer ticker.Stop()
		var lastDailyDate string

		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				utcNow := now.UTC()
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
	if s.Instances == nil || s.K8s == nil {
		return
	}

	instances, err := s.Instances.ListInstances(ctx)
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
	slog.Info("scheduler: starting daily backup", "instance", inst.Name, "num", inst.Number)

	if s.RconPool != nil {
		addr := fmt.Sprintf("%s.%s.svc.cluster.local:25575", inst.ServiceName(), s.namespace())
		client := s.RconPool.ClientFor(addr)
		_, _ = client.Execute("/save-off")
		_, _ = client.Execute("/save-all flush")
		defer func() { _, _ = client.Execute("/save-on") }()
	}

	jobName := fmt.Sprintf("mc-backup-%s-%02d-daily-%d", inst.Slug, inst.Number, time.Now().Unix())
	archiveName := backups.FormatBackupFileName(inst.Slug, inst.Number, "daily")

	if err := s.K8s.CreateBackupJob(ctx, jobName, archiveName, inst.PVCName(), s.backupsPVC()); err != nil {
		slog.Error("scheduler: failed to create daily backup job", "instance", inst.Name, "err", err)
		return
	}

	if s.Cfg != nil && s.Cfg.BackupsDir != "" {
		_ = backups.PruneBackups(s.Cfg.BackupsDir, inst.Slug, inst.Number, s.keep())
	}
	if s.Store != nil {
		_ = s.Store.RecordAudit("system", "mc-backup-daily", fmt.Sprintf("Daily backup created: %s", archiveName))
		_ = s.Store.RecordEvent("mc-backup-daily", fmt.Sprintf("World #%02d %s", inst.Number, inst.Name))
	}
}
