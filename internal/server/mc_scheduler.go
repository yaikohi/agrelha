package server

import (
	"context"

	"agrelha/internal/app"
)

// backupScheduler builds the application service from the server's wiring. The
// scheduler is not an HTTP concern — it used to live in internal/http/instances
// despite having no fiber dependency at all.
func (s *FiberServer) backupScheduler() *app.BackupScheduler {
	var ns string
	if s.cfg != nil {
		ns = s.cfg.MinecraftNamespace
	}
	return &app.BackupScheduler{
		Cfg:       s.cfg,
		Store:     s.store,
		Instances: s.mcInstances,
		K8s:       s.mck8s,
		RconPool:  s.mcRconPool,
		Namespace: ns,
	}
}

// StartMinecraftScheduler runs daily backups and pruning at 04:00 UTC.
func (s *FiberServer) StartMinecraftScheduler(ctx context.Context) {
	s.backupScheduler().Start(ctx)
}

func (s *FiberServer) runDailyBackups(ctx context.Context) {
	s.backupScheduler().RunDaily(ctx)
}
