package ports

import (
	"context"
	"errors"

	"agrelha/internal/domain"
)

// ErrPackageNotFound means the upstream catalogue has no such package, as
// distinct from the catalogue being unreachable. An installed mod that returns
// this is already broken: the next boot cannot fetch it (see ADR 0003).
var ErrPackageNotFound = errors.New("package not found")

// Game defines a game engine supported by agrelha.
type Game interface {
	// Runtime shape — consumed by whichever adapter is active
	RuntimeSpec(inst domain.Instance) domain.RuntimeSpec

	// Telemetry & Status
	Telemetry(ctx context.Context) (domain.GameTelemetry, error)

	// Client bundle export
	ExportClientBundle(ctx context.Context, inst domain.Instance) (domain.Bundle, error)
}

// PackageCatalog provides search, metadata, and dependency resolution for mod packages.
type PackageCatalog interface {
	Get(fullName string) (domain.ModSearchResult, bool)
	Search(ctx context.Context, query string, limit int) ([]domain.ModSearchResult, error)
	Ready() bool
	LatestVersion(ctx context.Context, ns, name string) (string, []string, error)
	Readme(ctx context.Context, ns, name, version string) (string, error)
	ResolveTree(ctx context.Context, ns, name string) ([]string, error)
}
