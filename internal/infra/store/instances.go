package store

import (
	"time"

	"agrelha/internal/domain"
)

type InstanceRecord struct {
	Number          int
	Name            string
	Slug            string
	Seed            string
	Password        string
	Loader          string
	Source          string
	Pack            string
	PackProvider    string
	PackRef         string
	MCVersion       string
	Tier            string
	State           string
	MOTD            string
	Difficulty      string
	Gamemode        string
	WorldType       string
	MaxPlayers      int
	LBIP            string
	MemRequestGiB   int
	MemLimitGiB     int
	CPURequestMilli int
	CPULimitMilli   int
	HeapInitGiB     int
	CreatedBy       string
	CreatedAt       time.Time
	LastUsed        time.Time
}

// The per-game methods below are thin wrappers over the table-driven
// implementation in instance_tables.go. The SQL lives there, described once as
// column lists, so a new game declares a table rather than adding a branch to
// each of these.

func (s *Store) UpsertInstance(inst InstanceRecord) error {
	return s.upsertInstanceRow(domain.GameMinecraft, inst)
}

func (s *Store) GetInstance(number int) (*InstanceRecord, error) {
	return s.getInstanceRow(domain.GameMinecraft, number)
}

func (s *Store) ListInstances() ([]InstanceRecord, error) {
	return s.listInstanceRows(domain.GameMinecraft)
}

func (s *Store) UpdateInstanceState(number int, state string) error {
	return s.updateInstanceRowState(domain.GameMinecraft, number, state)
}

func (s *Store) DeleteInstance(number int) error {
	return s.deleteInstanceRow(domain.GameMinecraft, number)
}

func (s *Store) UpsertValheimInstance(inst InstanceRecord) error {
	return s.upsertInstanceRow(domain.GameValheim, inst)
}

func (s *Store) GetValheimInstance(number int) (*InstanceRecord, error) {
	return s.getInstanceRow(domain.GameValheim, number)
}

func (s *Store) ListValheimInstances() ([]InstanceRecord, error) {
	return s.listInstanceRows(domain.GameValheim)
}

func (s *Store) UpdateValheimInstanceState(number int, state string) error {
	return s.updateInstanceRowState(domain.GameValheim, number, state)
}

func (s *Store) DeleteValheimInstance(number int) error {
	return s.deleteInstanceRow(domain.GameValheim, number)
}

// ValheimInstancesMissingSource lists instance numbers whose Source column was
// never written. It exists because the repository normalises an empty Source to
// modlist on read, so a caller can never tell "unset" from "explicitly modded" -
// which silently defeated the one-time backfill.
func (s *Store) ValheimInstancesMissingSource() ([]int, error) {
	rows, err := s.db.Query(`SELECT number FROM valheim_instances WHERE COALESCE(source,'') = '' ORDER BY number`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
