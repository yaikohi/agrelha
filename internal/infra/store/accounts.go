package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

var _ ports.Accounts = (*Store)(nil)

func (s *Store) UpsertAccount(ctx context.Context, a domain.Account) error {
	if a.Subject == "" {
		return errors.New("upsert account: empty subject")
	}
	now := a.LastSeen
	if now.IsZero() {
		now = time.Now()
	}
	first := a.FirstSeen
	if first.IsZero() {
		first = now
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO accounts (subject, email, name, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(subject) DO UPDATE SET
			email     = excluded.email,
			name      = excluded.name,
			last_seen = excluded.last_seen
	`, a.Subject, a.Email, a.Name, first, now)
	if err != nil {
		return fmt.Errorf("upsert account %q: %w", a.Subject, err)
	}
	return nil
}

func (s *Store) GetAccount(ctx context.Context, subject string) (*domain.Account, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT subject, email, name, first_seen, last_seen
		FROM accounts
		WHERE subject = ?
	`, subject)

	var a domain.Account
	if err := row.Scan(&a.Subject, &a.Email, &a.Name, &a.FirstSeen, &a.LastSeen); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get account %q: %w", subject, err)
	}
	return &a, nil
}

func (s *Store) ListAccounts(ctx context.Context) ([]domain.Account, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT subject, email, name, first_seen, last_seen
		FROM accounts
		ORDER BY email ASC, subject ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()

	var out []domain.Account
	for rows.Next() {
		var a domain.Account
		if err := rows.Scan(&a.Subject, &a.Email, &a.Name, &a.FirstSeen, &a.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
