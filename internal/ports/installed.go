package ports

import (
	"context"

	"agrelha/internal/domain"
)

// InstalledBuilds reports what a server actually has on disk.
//
// Distinct from what a Pack publishes: an Instance keeps the build it installed
// until something reinstalls it, so the Pack's current release can be newer
// than what is running. For an Instance that already exists this is the only
// source that cannot be wrong.
type InstalledBuilds interface {
	InstalledBuild(ctx context.Context, id domain.InstanceID) (domain.PackFacts, error)
}
