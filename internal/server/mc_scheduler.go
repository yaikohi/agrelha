package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"agrelha/internal/minecraft"
)

// StartMinecraftScheduler launches a background goroutine that performs daily backups and pruning at 04:00 AM UTC.
func (s *FiberServer) StartMinecraftScheduler(ctx context.Context) {
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

				// Run at 04:00 AM UTC once per calendar day
				if utcNow.Hour() == 4 && lastDailyDate != currentDate {
					lastDailyDate = currentDate
					s.runDailyBackups(ctx)
				}
			}
		}
	}()
}

func (s *FiberServer) runDailyBackups(ctx context.Context) {
	if s.mcInstances == nil || s.mck8s == nil {
		return
	}

	instances, err := s.mcInstances.ListInstances(ctx)
	if err != nil {
		slog.Error("scheduler: failed to list minecraft instances", "err", err)
		return
	}

	for _, inst := range instances {
		if inst.State != minecraft.StateRunning {
			continue
		}

		slog.Info("scheduler: starting daily backup", "instance", inst.Name, "num", inst.Number)

		// 1. RCON save-all flush if server is active
		if s.mcRconPool != nil {
			addr := fmt.Sprintf("%s.minecraft-modded.svc.cluster.local:25575", inst.ServiceName())
			client := s.mcRconPool.ClientFor(addr)
			_, _ = client.Execute("/save-off")
			_, _ = client.Execute("/save-all flush")
			defer func(c *minecraft.RconClient) {
				_, _ = c.Execute("/save-on")
			}(client)
		}

		// 2. Launch backup job
		jobName := fmt.Sprintf("mc-backup-%s-%02d-daily-%d", inst.Slug, inst.Number, time.Now().Unix())
		archiveName := minecraft.FormatBackupFileName(inst.Slug, inst.Number, "daily")
		backupsPVC := "minecraft-modded-backups"

		if err := s.mck8s.CreateBackupJob(ctx, jobName, archiveName, inst.PVCName(), backupsPVC); err != nil {
			slog.Error("scheduler: failed to create daily backup job", "instance", inst.Name, "err", err)
			continue
		}

		// 3. Prune older backups, keeping latest 5
		if s.cfg != nil && s.cfg.BackupsDir != "" {
			_ = minecraft.PruneBackups(s.cfg.BackupsDir, inst.Slug, inst.Number, 5)
		}

		_ = s.store.RecordAudit("system", "mc-backup-daily", fmt.Sprintf("Daily backup created: %s", archiveName))
		_ = s.store.RecordEvent("mc-backup-daily", fmt.Sprintf("World #%02d %s", inst.Number, inst.Name))
	}
}
