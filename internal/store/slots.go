package store

import "time"

type Slot struct {
	Slot         string
	Loader       string
	Source       string
	Pack         string
	PackProvider string
	PackRef      string
	MCVersion    string
	CreatedAt    time.Time
	LastUsed     time.Time
}

func (s *Store) UpsertSlot(sl Slot) error {
	_, err := s.db.Exec(`
		INSERT INTO mc_slots (slot, loader, source, pack, pack_provider, pack_ref, mc_version, last_used)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(slot) DO UPDATE SET
			loader        = excluded.loader,
			source        = excluded.source,
			pack          = COALESCE(NULLIF(excluded.pack, ''), mc_slots.pack),
			pack_provider = COALESCE(NULLIF(excluded.pack_provider, ''), mc_slots.pack_provider),
			pack_ref      = COALESCE(NULLIF(excluded.pack_ref, ''), mc_slots.pack_ref),
			mc_version    = COALESCE(NULLIF(excluded.mc_version, ''), mc_slots.mc_version),
			last_used     = CURRENT_TIMESTAMP`,
		sl.Slot, sl.Loader, sl.Source, sl.Pack, sl.PackProvider, sl.PackRef, sl.MCVersion)
	return err
}

func (s *Store) ListSlots() ([]Slot, error) {
	rows, err := s.db.Query(`
		SELECT slot, loader, source, COALESCE(pack,''), COALESCE(pack_provider,''),
		       COALESCE(pack_ref,''), COALESCE(mc_version,''), created_at, last_used
		FROM mc_slots ORDER BY last_used DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Slot
	for rows.Next() {
		var sl Slot
		var created, used any
		if err := rows.Scan(&sl.Slot, &sl.Loader, &sl.Source, &sl.Pack, &sl.PackProvider,
			&sl.PackRef, &sl.MCVersion, &created, &used); err != nil {
			return nil, err
		}
		sl.CreatedAt = asTime(created)
		sl.LastUsed = asTime(used)
		out = append(out, sl)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSlot(slot string) error {
	_, err := s.db.Exec(`DELETE FROM mc_slots WHERE slot = ?`, slot)
	return err
}
