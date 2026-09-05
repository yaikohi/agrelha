package store

import "time"

type HistoryEntry struct {
	At     time.Time
	Source string
	Kind   string
	Actor  string
	Detail string
}

func (s *Store) ListHistory(limit int) ([]HistoryEntry, error) {
	rows, err := s.db.Query(`
		SELECT at, 'action' AS source, action AS kind, COALESCE(actor,'') AS actor, COALESCE(detail,'') AS detail
		FROM audit
		UNION ALL
		SELECT at, 'event' AS source, kind AS kind, '' AS actor, COALESCE(detail,'') AS detail
		FROM events WHERE kind NOT IN ('restart','stop','start','mc-restart','mc-stop','mc-start')
		ORDER BY at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []HistoryEntry
	for rows.Next() {
		var h HistoryEntry
		var at any
		if err := rows.Scan(&at, &h.Source, &h.Kind, &h.Actor, &h.Detail); err != nil {
			return nil, err
		}
		h.At = asTime(at)
		out = append(out, h)
	}
	return out, rows.Err()
}

func asTime(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case []byte:
		return asTime(string(t))
	case string:
		for _, f := range []string{
			"2006-01-02 15:04:05.999999999-07:00",
			"2006-01-02 15:04:05",
			time.RFC3339,
		} {
			if ts, err := time.Parse(f, t); err == nil {
				return ts
			}
		}
	}
	return time.Time{}
}
