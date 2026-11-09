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

// InstalledPack is the Pack a server installed, read off the installer's own
// record rather than asked of the publisher again: a Pack world keeps the
// release it installed until something reinstalls it, and that exact release is
// what a player must import to be able to join.
type InstalledPack struct {
	Provider domain.Provider
	ModID    int
	FileID   int
	Name     string
	Version  string
	FileName string
}

// PackReader reports which Pack release a server has on disk.
type PackReader interface {
	InstalledPack(ctx context.Context, id domain.InstanceID) (InstalledPack, error)
}
