package store

import "time"

type Slot struct {
	Slot      string
	Type      string
	Pack      string
	MCVersion string
	CFPageURL string
	CreatedAt time.Time
	LastUsed  time.Time
}

func (s *Store) UpsertSlot(sl Slot) error {
	_, err := s.db.Exec(`
		INSERT INTO mc_slots (slot, type, pack, mc_version, cf_page_url, last_used)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(slot) DO UPDATE SET
			type        = excluded.type,
			pack        = COALESCE(NULLIF(excluded.pack, ''), mc_slots.pack),
			mc_version  = COALESCE(NULLIF(excluded.mc_version, ''), mc_slots.mc_version),
			cf_page_url = COALESCE(NULLIF(excluded.cf_page_url, ''), mc_slots.cf_page_url),
			last_used   = CURRENT_TIMESTAMP`,
		sl.Slot, sl.Type, sl.Pack, sl.MCVersion, sl.CFPageURL)
	return err
}

func (s *Store) ListSlots() ([]Slot, error) {
	rows, err := s.db.Query(`
		SELECT slot, type, COALESCE(pack,''), COALESCE(mc_version,''),
		       COALESCE(cf_page_url,''), created_at, last_used
		FROM mc_slots ORDER BY last_used DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Slot
	for rows.Next() {
		var sl Slot
		var created, used any
		if err := rows.Scan(&sl.Slot, &sl.Type, &sl.Pack, &sl.MCVersion, &sl.CFPageURL, &created, &used); err != nil {
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
