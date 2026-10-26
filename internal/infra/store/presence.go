package store

func (s *Store) SetOnline(instance int, steamID string, online bool) error {
	if online {
		if instance > 0 {
			if _, err := s.db.Exec(`
				INSERT INTO instance_presence (instance_number, steam_id, online, online_since)
				VALUES (?, ?, 1, CURRENT_TIMESTAMP)
				ON CONFLICT(instance_number, steam_id) DO UPDATE SET
					online = 1,
					online_since = COALESCE(instance_presence.online_since, CURRENT_TIMESTAMP)`,
				instance, steamID); err != nil {
				return err
			}
		}
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

	if instance > 0 {
		if _, err := s.db.Exec(`DELETE FROM instance_presence WHERE instance_number = ? AND steam_id = ?`, instance, steamID); err != nil {
			return err
		}
	} else {
		if _, err := s.db.Exec(`DELETE FROM instance_presence WHERE steam_id = ?`, steamID); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(`
		UPDATE players SET
			online = CASE WHEN EXISTS (SELECT 1 FROM instance_presence WHERE steam_id = ? AND online = 1) THEN 1 ELSE 0 END,
			online_since = (SELECT MIN(online_since) FROM instance_presence WHERE steam_id = ? AND online = 1),
			last_seen = CURRENT_TIMESTAMP
		WHERE steam_id = ?`,
		steamID, steamID, steamID)
	return err
}

func (s *Store) ClearPresence(instance int) error {
	if instance > 0 {
		if _, err := s.db.Exec(`DELETE FROM instance_presence WHERE instance_number = ?`, instance); err != nil {
			return err
		}
		_, err := s.db.Exec(`
			UPDATE players SET
				online = CASE WHEN EXISTS (SELECT 1 FROM instance_presence WHERE steam_id = players.steam_id AND online = 1) THEN 1 ELSE 0 END,
				online_since = (SELECT MIN(online_since) FROM instance_presence WHERE steam_id = players.steam_id AND online = 1)
		`)
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM instance_presence`); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE players SET online = 0, online_since = NULL WHERE online = 1`)
	return err
}

func (s *Store) CountOnline() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM players WHERE online = 1`).Scan(&n)
	return n, err
}

func (s *Store) CountOnlineForInstance(instance int) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM instance_presence WHERE online = 1 AND instance_number = ?`, instance).Scan(&n)
	return n, err
}
