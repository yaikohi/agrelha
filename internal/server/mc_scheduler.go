package server

import (
	"context"
	"fmt"

	appbackups "agrelha/internal/app/backups"
	"agrelha/internal/domain"
	infrabackups "agrelha/internal/infra/backups"
)

// backupScheduler builds the application service from the server's wiring. The
// scheduler is not an HTTP concern — it used to live in internal/http/instances
// despite having no fiber dependency at all.
func (s *FiberServer) backupScheduler() *appbackups.BackupScheduler {
	var opts []appbackups.Option
	if s.cfg != nil {
		opts = append(opts,
			appbackups.WithNamespace(s.cfg.MinecraftNamespace),
		)
		if s.cfg.BackupsDir != "" {
			opts = append(opts, appbackups.WithPruner(func(slug string, num, keep int) error {
				return infrabackups.PruneBackups(s.cfg.BackupsDir, slug, num, keep)
			}))
		}
	}
	if s.store != nil {
		opts = append(opts,
			appbackups.WithAudit(s.store),
			appbackups.WithEvent(s.store),
		)
	}
	if s.mcInstances != nil {
		opts = append(opts, appbackups.WithCommandExecutor(func(ctx context.Context, inst domain.Instance, cmd string) error {
			_, err := s.mcInstances.ExecuteCommand(ctx, inst.Number, cmd)
			return err
		}))
	} else if s.mcRconPool != nil && s.cfg != nil {
		opts = append(opts, appbackups.WithCommandExecutor(func(ctx context.Context, inst domain.Instance, cmd string) error {
			addr := fmt.Sprintf("%s.%s.svc.cluster.local:25575", inst.ServiceName(), s.cfg.MinecraftNamespace)
			_, err := s.mcRconPool.ClientFor(addr).Execute(cmd)
			return err
		}))
	}
	return appbackups.New(s.mcInstances, s.mck8s, opts...)
}

// StartMinecraftScheduler runs daily backups and pruning at 04:00 UTC.
func (s *FiberServer) StartMinecraftScheduler(ctx context.Context) {
	s.backupScheduler().Start(ctx)
}

func (s *FiberServer) runDailyBackups(ctx context.Context) {
	s.backupScheduler().RunDaily(ctx)
}
