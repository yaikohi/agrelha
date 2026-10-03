package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

var _ ports.InstanceRequests = (*Store)(nil)

type requestSpec struct {
	Instance domain.Instance `json:"instance"`
	Mods     domain.ModList  `json:"mods"`
}

func (s *Store) PutRequest(ctx context.Context, r domain.InstanceRequest) error {
	if r.ID == "" || r.Subject == "" {
		return errors.New("put request: empty id or subject")
	}
	spec, err := json.Marshal(requestSpec{Instance: r.Instance, Mods: r.Mods})
	if err != nil {
		return fmt.Errorf("put request: encode spec: %w", err)
	}
	created := r.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	var decided any
	if !r.DecidedAt.IsZero() {
		decided = r.DecidedAt
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO instance_requests (id, subject, game_id, name, spec, status, note, created_at, decided_at, decided_by, number)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status     = excluded.status,
			note       = excluded.note,
			decided_at = excluded.decided_at,
			decided_by = excluded.decided_by,
			number     = excluded.number
	`, r.ID, r.Subject, string(r.GameID), r.Name, string(spec), string(r.Status), r.Note, created, decided, r.DecidedBy, r.Number)
	if err != nil {
		return fmt.Errorf("put request %q: %w", r.ID, err)
	}
	return nil
}

func scanRequest(sc interface{ Scan(...any) error }) (domain.InstanceRequest, error) {
	var r domain.InstanceRequest
	var game, status, spec string
	var created any
	var decided sql.NullTime
	if err := sc.Scan(&r.ID, &r.Subject, &game, &r.Name, &spec, &status, &r.Note, &created, &decided, &r.DecidedBy, &r.Number); err != nil {
		return r, err
	}
	r.GameID = domain.GameID(game)
	r.Status = domain.RequestStatus(status)
	r.CreatedAt = asTime(created)
	if decided.Valid {
		r.DecidedAt = decided.Time
	}
	var sp requestSpec
	if err := json.Unmarshal([]byte(spec), &sp); err != nil {
		return r, fmt.Errorf("decode request spec %q: %w", r.ID, err)
	}
	r.Instance = sp.Instance
	r.Mods = sp.Mods
	return r, nil
}

const requestCols = `id, subject, game_id, name, spec, status, note, created_at, decided_at, COALESCE(decided_by,''), COALESCE(number,0)`

func (s *Store) GetRequest(ctx context.Context, id string) (*domain.InstanceRequest, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+requestCols+` FROM instance_requests WHERE id = ?`, id)
	r, err := scanRequest(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get request %q: %w", id, err)
	}
	return &r, nil
}

func (s *Store) listRequests(ctx context.Context, where string, args ...any) ([]domain.InstanceRequest, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+requestCols+` FROM instance_requests `+where+` ORDER BY created_at ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("list requests: %w", err)
	}
	defer rows.Close()
	var out []domain.InstanceRequest
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ListRequests(ctx context.Context, status domain.RequestStatus) ([]domain.InstanceRequest, error) {
	if status == "" {
		return s.listRequests(ctx, "")
	}
	return s.listRequests(ctx, "WHERE status = ?", string(status))
}

func (s *Store) ListRequestsFor(ctx context.Context, subject string) ([]domain.InstanceRequest, error) {
	return s.listRequests(ctx, "WHERE subject = ?", subject)
}
