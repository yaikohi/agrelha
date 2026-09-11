package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"agrelha/internal/domain"
)

func (s *Store) RecordIncident(ctx context.Context, in domain.Incident) (int64, error) {
	if in.At.IsZero() {
		in.At = time.Now()
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO incidents (game_id, number, at, restart_count, exit_code, reason, oom_killed, log_tail)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		string(in.GameID), in.Number, in.At, in.RestartCount, in.ExitCode, in.Reason, in.OOMKilled, in.LogTail)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) LastIncident(ctx context.Context, game domain.GameID, number int) (*domain.Incident, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, game_id, number, at, restart_count, exit_code, COALESCE(reason,''), oom_killed, COALESCE(log_tail,'')
		 FROM incidents WHERE game_id = ? AND number = ? ORDER BY at DESC, id DESC LIMIT 1`,
		string(game), number)

	in, err := scanIncident(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &in, nil
}

func (s *Store) ListIncidents(ctx context.Context, game domain.GameID, number, limit int) ([]domain.Incident, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, game_id, number, at, restart_count, exit_code, COALESCE(reason,''), oom_killed, COALESCE(log_tail,'')
		 FROM incidents WHERE game_id = ? AND number = ? ORDER BY at DESC, id DESC LIMIT ?`,
		string(game), number, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Incident
	for rows.Next() {
		in, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanIncident(sc scanner) (domain.Incident, error) {
	var in domain.Incident
	var game string
	err := sc.Scan(&in.ID, &game, &in.Number, &in.At, &in.RestartCount, &in.ExitCode, &in.Reason, &in.OOMKilled, &in.LogTail)
	in.GameID = domain.GameID(game)
	return in, err
}
