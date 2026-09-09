package server

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/infra/backups"
	"agrelha/internal/web/shared"
)

// sseMain streams tile signals + the available-updates badge/list (every 5s)
// over one SSE connection.
func (s *FiberServer) sseMain(c *fiber.Ctx) error {
	return s.ensureDashboardHandler().SSEMain(c)
}

// sseLogs streams only the live log tail into #logs. Supports ?server=valheim (default) or ?server=minecraft.
func (s *FiberServer) sseLogs(c *fiber.Ctx) error {
	return s.ensureConsoleHandler().SSELogs(c)
}

func (s *FiberServer) tileSignals(ctx context.Context) map[string]any {
	return s.ensureDashboardHandler().TileSignals(ctx)
}

func (s *FiberServer) backupInfo() (backups.Info, bool) {
	return s.ensureBackupsHandler().BackupInfo()
}

func humanAgo(t time.Time) string {
	return shared.HumanAgo(t)
}

func humanSize(b int64) string {
	return shared.HumanSize(b)
}

func humanDuration(d time.Duration) string {
	return shared.HumanDuration(d)
}
