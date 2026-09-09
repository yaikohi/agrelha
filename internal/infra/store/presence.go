package store

func (s *Store) SetOnline(steamID string, online bool) error {
	if online {
		_, err := s.db.Exec(`
			INSERT INTO players (steam_id, online, online_since)
			VALUES (?, 1, CURRENT_TIMESTAMP)
			ON CONFLICT(steam_id) DO UPDATE SET
				online = 1,
				online_since = COALESCE(players.online_since, CURRENT_TIMESTAMP),
				last_seen = CURRENT_TIMESTAMP`,
			steamID)
		return err
	}
	_, err := s.db.Exec(`
		UPDATE players SET online = 0, online_since = NULL, last_seen = CURRENT_TIMESTAMP
		WHERE steam_id = ?`, steamID)
	return err
}

func (s *Store) ClearPresence() error {
	_, err := s.db.Exec(`UPDATE players SET online = 0, online_since = NULL WHERE online = 1`)
	return err
}

func (s *Store) CountOnline() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM players WHERE online = 1`).Scan(&n)
	return n, err
}
