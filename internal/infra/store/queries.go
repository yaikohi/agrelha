package store

import (
	"database/sql"

	"agrelha/internal/domain"
)

type Player = domain.Player

func (s *Store) UpsertSeen(instance int, steamID, character string, bumpSession bool) error {
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
	if err != nil {
		return err
	}
	if instance > 0 && bumpSession {
		if _, err := s.db.Exec(`
			INSERT INTO instance_presence (instance_number, steam_id, online, online_since)
			VALUES (?, ?, 1, CURRENT_TIMESTAMP)
			ON CONFLICT(instance_number, steam_id) DO UPDATE SET
				online = 1,
				online_since = COALESCE(instance_presence.online_since, CURRENT_TIMESTAMP)`,
			instance, steamID); err != nil {
			return err
		}
		_, err = s.db.Exec(`
			UPDATE players SET
				online = 1,
				online_since = COALESCE(online_since, CURRENT_TIMESTAMP),
				last_seen = CURRENT_TIMESTAMP
			WHERE steam_id = ?`, steamID)
		return err
	}
	return nil
}

func (s *Store) ListPlayers() ([]Player, error) {
	rows, err := s.db.Query(`
		SELECT p.steam_id, COALESCE(p.character,''), p.first_seen, p.last_seen, p.sessions,
		       p.online, p.online_since,
		       COALESCE((SELECT ip.instance_number FROM instance_presence ip WHERE ip.steam_id = p.steam_id AND ip.online = 1 LIMIT 1), 0)
		FROM players p ORDER BY p.online DESC, p.last_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPlayers(rows)
}

func (s *Store) ListPlayersForInstance(instance int) ([]Player, error) {
	rows, err := s.db.Query(`
		SELECT p.steam_id, COALESCE(p.character,''), p.first_seen, p.last_seen, p.sessions,
		       COALESCE(ip.online, 0), ip.online_since, COALESCE(ip.instance_number, 0)
		FROM players p
		JOIN instance_presence ip ON p.steam_id = ip.steam_id AND ip.online = 1 AND ip.instance_number = ?
		ORDER BY p.last_seen DESC`, instance)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPlayers(rows)
}

func scanPlayers(rows *sql.Rows) ([]Player, error) {
	var out []Player
	for rows.Next() {
		var p Player
		var online int
		var since sql.NullTime
		if err := rows.Scan(&p.SteamID, &p.Character, &p.FirstSeen, &p.LastSeen, &p.Sessions, &online, &since, &p.Instance); err != nil {
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
