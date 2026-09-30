package store

import (
	"sort"
	"strconv"
	"time"

	"agrelha/internal/domain"
)

type HistoryEntry = domain.HistoryEntry

func (s *Store) ListHistory(limit int) ([]HistoryEntry, error) {
	entries, err := s.listTimeline(limit)
	if err != nil {
		return nil, err
	}
	incidents, err := s.listIncidentHistory(limit)
	if err != nil {
		return entries, nil
	}
	entries = append(entries, incidents...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].At.After(entries[j].At) })
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func (s *Store) listIncidentHistory(limit int) ([]HistoryEntry, error) {
	rows, err := s.db.Query(`
		SELECT id, game_id, number, at, restart_count, exit_code,
		       COALESCE(reason,''), oom_killed, COALESCE(log_tail,'')
		FROM incidents ORDER BY at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []HistoryEntry
	for rows.Next() {
		var in domain.Incident
		var at any
		var oom int
		if err := rows.Scan(&in.ID, &in.GameID, &in.Number, &at, &in.RestartCount,
			&in.ExitCode, &in.Reason, &oom, &in.LogTail); err != nil {
			return nil, err
		}
		in.At = asTime(at)
		in.OOMKilled = oom != 0
		detail := in.Reason
		if detail == "" {
			detail = "exit " + strconv.Itoa(int(in.ExitCode))
		}
		if in.OOMKilled {
			detail = "out of memory (" + detail + ")"
		}
		copyIn := in
		out = append(out, HistoryEntry{
			At:       in.At,
			Source:   "incident",
			Kind:     "crash",
			Detail:   detail,
			Incident: &copyIn,
		})
	}
	return out, rows.Err()
}

func (s *Store) PruneEvents(before time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM events WHERE at < ?`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) listTimeline(limit int) ([]HistoryEntry, error) {
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
