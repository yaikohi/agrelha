package store

import (
	"fmt"
	"log/slog"

	"agrelha/internal/domain"
)

func (s *Store) backfillInstanceResources() error {
	for _, profile := range domain.Profiles() {
		n, err := s.backfillTableResources(profile)
		if err != nil {
			return fmt.Errorf("backfill resources for %s: %w", profile.ID, err)
		}
		if n > 0 {
			slog.Info("instance resources backfilled from legacy tier sizes",
				"game", profile.ID, "instances", n)
		}
	}
	return nil
}

func (s *Store) backfillTableResources(profile domain.GameProfile) (int, error) {
	rows, err := s.db.Query(
		`SELECT number, COALESCE(tier,'') FROM ` + profile.Table +
			` WHERE COALESCE(mem_request_gib,0) = 0`)
	if err != nil {
		return 0, err
	}

	type pending struct {
		number int
		tier   domain.ResourceTier
	}
	var todo []pending
	for rows.Next() {
		var number int
		var tier string
		if err := rows.Scan(&number, &tier); err != nil {
			rows.Close()
			return 0, err
		}
		todo = append(todo, pending{number: number, tier: domain.NormalizeTier(tier)})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	count := 0
	for _, p := range todo {
		r := domain.LegacyResources(profile.ID, p.tier)
		if r.IsZero() {
			slog.Warn("instance has no stored resources and its game has no legacy sizes; left untouched",
				"game", profile.ID, "number", p.number)
			continue
		}
		if _, err := s.db.Exec(
			`UPDATE `+profile.Table+` SET mem_request_gib = ?, mem_limit_gib = ?,
			        cpu_request_milli = ?, cpu_limit_milli = ? WHERE number = ?`,
			r.MemRequestGiB, r.MemLimitGiB, r.CPURequestMilli, r.CPULimitMilli, p.number); err != nil {
			return count, err
		}
		if heap := domain.LegacyHeapInitGiB(profile.ID, p.tier); heap > 0 {
			if _, err := s.db.Exec(
				`UPDATE `+profile.Table+` SET heap_init_gib = ? WHERE number = ?`,
				heap, p.number); err != nil {
				return count, err
			}
		}
		count++
	}
	return count, nil
}

func (s *Store) backfillWorldIDs() error {
	for _, profile := range domain.Profiles() {
		n, err := s.backfillTableWorldIDs(profile)
		if err != nil {
			return fmt.Errorf("backfill world_id for %s: %w", profile.ID, err)
		}
		if n > 0 {
			slog.Info("instance world_id backfilled", "game", profile.ID, "instances", n)
		}
	}

	if err := s.sweepOrphanedEphemeralRows(); err != nil {
		return fmt.Errorf("sweep orphaned ephemeral rows: %w", err)
	}

	return nil
}

func (s *Store) backfillTableWorldIDs(profile domain.GameProfile) (int, error) {
	rows, err := s.db.Query(
		`SELECT number, created_at FROM ` + profile.Table +
			` WHERE COALESCE(world_id, '') = ''`)
	if err != nil {
		return 0, err
	}

	type pending struct {
		number    int
		createdAt any
	}
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.number, &p.createdAt); err != nil {
			rows.Close()
			return 0, err
		}
		todo = append(todo, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	count := 0
	for _, p := range todo {
		wid := domain.NewID()
		if _, err := s.db.Exec(
			`UPDATE `+profile.Table+` SET world_id = ? WHERE number = ?`,
			wid, p.number); err != nil {
			return count, err
		}
		occupantCreated := asTime(p.createdAt).UTC()

		incRows, err := s.db.Query(
			`SELECT id, at FROM incidents
			 WHERE game_id = ? AND number = ? AND COALESCE(world_id, '') = ''`,
			string(profile.ID), p.number)
		if err != nil {
			return count, err
		}
		type incItem struct {
			id int64
			at any
		}
		var toUpdate []int64
		for incRows.Next() {
			var item incItem
			if err := incRows.Scan(&item.id, &item.at); err == nil {
				incAt := asTime(item.at).UTC()
				if !incAt.Before(occupantCreated) {
					toUpdate = append(toUpdate, item.id)
				}
			}
		}
		incRows.Close()

		for _, id := range toUpdate {
			if _, err := s.db.Exec(`UPDATE incidents SET world_id = ? WHERE id = ?`, wid, id); err != nil {
				return count, err
			}
		}
		count++
	}
	return count, nil
}

func (s *Store) sweepOrphanedEphemeralRows() error {
	// Purge orphaned mod_restore_points where slot has no active occupant in that game's table.
	for _, profile := range domain.Profiles() {
		if _, err := s.db.Exec(
			`DELETE FROM mod_restore_points WHERE game_id = ? AND number NOT IN (SELECT number FROM `+profile.Table+`)`,
			string(profile.ID)); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(`DELETE FROM mod_restore_points WHERE game_id NOT IN ('minecraft', 'valheim', 'gmod')`); err != nil {
		return err
	}

	// Purge orphaned instance_presence where slot has no active occupant in valheim_instances.
	if _, err := s.db.Exec(`DELETE FROM instance_presence WHERE instance_number NOT IN (SELECT number FROM valheim_instances)`); err != nil {
		return err
	}
	return nil
}
