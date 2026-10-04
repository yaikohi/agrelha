package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

var (
	_ ports.Tiers             = (*Store)(nil)
	_ ports.GameSettingsStore = (*Store)(nil)
)

func (s *Store) ListTiers(ctx context.Context, gameID domain.GameID) ([]domain.Tier, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT key, name, mem_request_gib, mem_limit_gib, cpu_request_milli,
		       cpu_limit_milli, heap_init_gib, sort_order
		FROM tiers WHERE game_id = ? ORDER BY sort_order ASC, key ASC`, string(gameID))
	if err != nil {
		return nil, fmt.Errorf("list tiers for %s: %w", gameID, err)
	}
	defer rows.Close()

	var out []domain.Tier
	for rows.Next() {
		t := domain.Tier{GameID: gameID}
		if err := rows.Scan(&t.Key, &t.Name, &t.Resources.MemRequestGiB, &t.Resources.MemLimitGiB,
			&t.Resources.CPURequestMilli, &t.Resources.CPULimitMilli, &t.HeapInitGiB, &t.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) PutTier(ctx context.Context, t domain.Tier) error {
	if t.GameID == "" || t.Key == "" {
		return errors.New("put tier: empty game or key")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tiers (game_id, key, name, mem_request_gib, mem_limit_gib,
		                   cpu_request_milli, cpu_limit_milli, heap_init_gib, sort_order)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(game_id, key) DO UPDATE SET
			name              = excluded.name,
			mem_request_gib   = excluded.mem_request_gib,
			mem_limit_gib     = excluded.mem_limit_gib,
			cpu_request_milli = excluded.cpu_request_milli,
			cpu_limit_milli   = excluded.cpu_limit_milli,
			heap_init_gib     = excluded.heap_init_gib,
			sort_order        = excluded.sort_order`,
		string(t.GameID), t.Key, t.Name, t.Resources.MemRequestGiB, t.Resources.MemLimitGiB,
		t.Resources.CPURequestMilli, t.Resources.CPULimitMilli, t.HeapInitGiB, t.SortOrder)
	if err != nil {
		return fmt.Errorf("put tier %s/%s: %w", t.GameID, t.Key, err)
	}
	return nil
}

func (s *Store) DeleteTier(ctx context.Context, gameID domain.GameID, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tiers WHERE game_id = ? AND key = ?`, string(gameID), key)
	if err != nil {
		return fmt.Errorf("delete tier %s/%s: %w", gameID, key, err)
	}
	return nil
}

func (s *Store) GetGameSettings(ctx context.Context, gameID domain.GameID) (*domain.GameSettings, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT total_budget_gib, max_instances, max_running, ceiling_mem_gib, ceiling_cpu_milli
		FROM game_settings WHERE game_id = ?`, string(gameID))

	g := domain.GameSettings{GameID: gameID}
	if err := row.Scan(&g.TotalBudgetGiB, &g.MaxInstances, &g.MaxRunning,
		&g.Ceiling.MemLimitGiB, &g.Ceiling.CPULimitMilli); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get settings for %s: %w", gameID, err)
	}
	return &g, nil
}

func (s *Store) PutGameSettings(ctx context.Context, g domain.GameSettings) error {
	if g.GameID == "" {
		return errors.New("put game settings: empty game")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO game_settings (game_id, total_budget_gib, max_instances, max_running,
		                           ceiling_mem_gib, ceiling_cpu_milli, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(game_id) DO UPDATE SET
			total_budget_gib  = excluded.total_budget_gib,
			max_instances     = excluded.max_instances,
			max_running       = excluded.max_running,
			ceiling_mem_gib   = excluded.ceiling_mem_gib,
			ceiling_cpu_milli = excluded.ceiling_cpu_milli,
			updated_at        = CURRENT_TIMESTAMP`,
		string(g.GameID), g.TotalBudgetGiB, g.MaxInstances, g.MaxRunning,
		g.Ceiling.MemLimitGiB, g.Ceiling.CPULimitMilli)
	if err != nil {
		return fmt.Errorf("put settings for %s: %w", g.GameID, err)
	}
	return nil
}

var _ ports.GlobalSettings = (*Store)(nil)

func (s *Store) GetGlobalInt(ctx context.Context, key string) (int, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("get global %q: %w", key, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, false, fmt.Errorf("global %q is not a number: %q", key, raw)
	}
	return n, true, nil
}

func (s *Store) SetGlobalInt(ctx context.Context, key string, value int) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, strconv.Itoa(value))
	if err != nil {
		return fmt.Errorf("set global %q: %w", key, err)
	}
	return nil
}
