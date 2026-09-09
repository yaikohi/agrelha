package ports

import "agrelha/internal/domain"

// InstanceRepository persists Instances. It speaks domain types only — the
// adapter owns the storage record and the conversion to and from it, so the
// application layer never learns that persistence is SQLite.
//
// Before this port existed, internal/minecraft imported the store package
// directly: an application service depending concretely on a database.
type InstanceRepository interface {
	Upsert(inst domain.Instance) error
	Get(number int) (*domain.Instance, error)
	List() ([]domain.Instance, error)
	UpdateState(number int, state domain.InstanceState) error
	Delete(number int) error
}

// AuditRecorder records operational actions for auditing and history tracking.
type AuditRecorder interface {
	RecordAudit(actor, action, detail string) error
}

// EventRecorder records system events (joins, crashes, lifecycle transitions).
type EventRecorder interface {
	RecordEvent(kind, detail string) error
}

// HistoryReader lists recent history entries.
type HistoryReader interface {
	ListHistory(limit int) ([]domain.HistoryEntry, error)
}

// PlayerReader lists known players in the roster.
type PlayerReader interface {
	ListPlayers() ([]domain.Player, error)
}

// ReadmeCache provides cached markdown readmes for content packages.
type ReadmeCache interface {
	GetReadme(fullName, version string) (markdown string, hit bool, err error)
	PutReadme(fullName, version, markdown string) error
}
