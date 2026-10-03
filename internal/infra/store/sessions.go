package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

var _ ports.Sessions = (*Store)(nil)

func encodeRoles(roles []domain.Role) string {
	parts := make([]string, 0, len(roles))
	for _, r := range roles {
		if s := strings.TrimSpace(string(r)); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

func decodeRoles(s string) []domain.Role {
	var out []domain.Role
	for _, f := range strings.Fields(s) {
		out = append(out, domain.Role(f))
	}
	return out
}

func (s *Store) CreateSession(ctx context.Context, sess ports.Session) error {
	if sess.ID == "" || sess.Subject == "" {
		return errors.New("create session: empty id or subject")
	}
	created := sess.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, subject, email, name, roles, id_token, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, sess.ID, sess.Subject, sess.Email, sess.Name, encodeRoles(sess.Roles), sess.IDToken, created, sess.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *Store) LoadSession(ctx context.Context, id string) (*ports.Session, error) {
	if id == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, subject, email, name, roles, id_token, created_at, expires_at
		FROM sessions
		WHERE id = ?
	`, id)

	var sess ports.Session
	var roles string
	if err := row.Scan(&sess.ID, &sess.Subject, &sess.Email, &sess.Name, &roles, &sess.IDToken, &sess.CreatedAt, &sess.ExpiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load session: %w", err)
	}
	if !sess.ExpiresAt.After(time.Now()) {
		return nil, nil
	}
	sess.Roles = decodeRoles(roles)
	return &sess, nil
}

func (s *Store) TouchSession(ctx context.Context, id string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET expires_at = ? WHERE id = ?`, expiresAt, id)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Store) DeleteSessionsFor(ctx context.Context, subject string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE subject = ?`, subject); err != nil {
		return fmt.Errorf("delete sessions for %q: %w", subject, err)
	}
	return nil
}

func (s *Store) PurgeExpiredSessions(ctx context.Context, now time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now)
	if err != nil {
		return 0, fmt.Errorf("purge sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return int(n), nil
}
