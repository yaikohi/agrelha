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
