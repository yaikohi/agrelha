package store

import (
	"database/sql"

	"agrelha/internal/domain"
)

type Player = domain.Player

// UpsertSeen records/refreshes a player from a parsed join event. bumpSession
// increments the session counter (set true on a fresh connection line).
func (s *Store) UpsertSeen(steamID, character string, bumpSession bool) error {
	inc := 0
	if bumpSession {
		inc = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO players (steam_id, character, sessions)
		VALUES (?, NULLIF(?, ''), ?)
		ON CONFLICT(steam_id) DO UPDATE SET
			last_seen = CURRENT_TIMESTAMP,
			character = COALESCE(NULLIF(excluded.character, ''), players.character),
			sessions  = players.sessions + ?`,
		steamID, character, inc, inc)
	return err
}

// ListPlayers returns the roster, online players first then most-recently-seen.
func (s *Store) ListPlayers() ([]Player, error) {
	rows, err := s.db.Query(`
		SELECT steam_id, COALESCE(character,''), first_seen, last_seen, sessions, online, online_since
		FROM players ORDER BY online DESC, last_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Player
	for rows.Next() {
		var p Player
		var online int
		var since sql.NullTime
		if err := rows.Scan(&p.SteamID, &p.Character, &p.FirstSeen, &p.LastSeen, &p.Sessions, &online, &since); err != nil {
			return nil, err
		}
		p.Online = online != 0
		if since.Valid {
			p.OnlineSince = since.Time
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) RecordAudit(actor, action, detail string) error {
	_, err := s.db.Exec(`INSERT INTO audit (actor, action, detail) VALUES (?, ?, ?)`,
		actor, action, detail)
	return err
}

func (s *Store) RecordEvent(kind, detail string) error {
	_, err := s.db.Exec(`INSERT INTO events (kind, detail) VALUES (?, ?)`, kind, detail)
	return err
}
