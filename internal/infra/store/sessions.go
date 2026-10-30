package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
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
	for f := range strings.FieldsSeq(s) {
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

func (s *Store) mutateSessionRoles(ctx context.Context, subject string, apply func([]domain.Role) []domain.Role) error {
	query := `SELECT id, roles FROM sessions WHERE expires_at > ?`
	args := []any{time.Now()}
	if subject != "" {
		query += ` AND subject = ?`
		args = append(args, subject)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("read sessions for role update: %w", err)
	}

	type pending struct {
		id    string
		roles string
	}
	var todo []pending
	for rows.Next() {
		var id, roles string
		if err := rows.Scan(&id, &roles); err != nil {
			rows.Close()
			return fmt.Errorf("scan session for role update: %w", err)
		}
		next := encodeRoles(apply(decodeRoles(roles)))
		if next != roles {
			todo = append(todo, pending{id: id, roles: next})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read sessions for role update: %w", err)
	}
	rows.Close()

	for _, t := range todo {
		if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET roles = ? WHERE id = ?`, t.roles, t.id); err != nil {
			return fmt.Errorf("update session roles: %w", err)
		}
	}
	return nil
}

func (s *Store) AddSessionRole(ctx context.Context, subject string, role domain.Role) error {
	if subject == "" || role == "" {
		return nil
	}
	return s.mutateSessionRoles(ctx, subject, func(roles []domain.Role) []domain.Role {
		if slices.Contains(roles, role) {
			return roles
		}
		return append(roles, role)
	})
}

func (s *Store) RemoveSessionRole(ctx context.Context, subject string, role domain.Role) error {
	if subject == "" || role == "" {
		return nil
	}
	return s.mutateSessionRoles(ctx, subject, func(roles []domain.Role) []domain.Role {
		return slices.DeleteFunc(roles, func(r domain.Role) bool { return r == role })
	})
}

func (s *Store) RemoveSessionRoleEverywhere(ctx context.Context, role domain.Role) error {
	if role == "" {
		return nil
	}
	return s.mutateSessionRoles(ctx, "", func(roles []domain.Role) []domain.Role {
		return slices.DeleteFunc(roles, func(r domain.Role) bool { return r == role })
	})
}
