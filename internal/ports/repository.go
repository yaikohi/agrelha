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
