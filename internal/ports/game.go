package ports

import (
	"context"

	"agrelha/internal/domain"
)

// ContentProvider represents an upstream mod/addon source (e.g. Modrinth, Thunderstore).
type ContentProvider interface {
	ID() domain.Provider
	Search(ctx context.Context, query string) ([]domain.ContentItem, error)
}

// Game defines a game engine supported by agrelha.
type Game interface {
	ID() domain.GameID
	Display() domain.Display

	// Content
	Providers() []ContentProvider
	ResolveContent(ctx context.Context, inst domain.Instance) (domain.ContentSet, error)
	ExportClientBundle(ctx context.Context, inst domain.Instance) (domain.Bundle, error)

	// Runtime shape — consumed by whichever adapter is active
	RuntimeSpec(inst domain.Instance) domain.RuntimeSpec

	// Access
	AdmissionModel() domain.AdmissionModel
	OperatorIDKind() domain.OperatorIDKind
}
