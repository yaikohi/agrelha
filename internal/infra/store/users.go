package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"agrelha/internal/ports"
)

var _ ports.UserStore = (*Store)(nil)

// GetUser retrieves a user and their hashed password by username.
func (s *Store) GetUser(ctx context.Context, username string) (*ports.User, string, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT username, password_hash, email, created_at
		FROM users
		WHERE username = ?
	`, username)

	var u ports.User
	var hash string
	var email sql.NullString
	var createdAt time.Time

	if err := row.Scan(&u.Username, &hash, &email, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("get user %q: %w", username, err)
	}

	u.Email = email.String
	u.CreatedAt = createdAt
	return &u, hash, nil
}

// CreateUser creates or replaces a local user record.
func (s *Store) CreateUser(ctx context.Context, user ports.User, passwordHash string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, email, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET
			password_hash = excluded.password_hash,
			email         = excluded.email
	`, user.Username, passwordHash, user.Email, user.CreatedAt)
	if err != nil {
		return fmt.Errorf("create user %q: %w", user.Username, err)
	}
	return nil
}

// ListUsers returns all local users in alphabetical order.
func (s *Store) ListUsers(ctx context.Context) ([]ports.User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT username, email, created_at
		FROM users
		ORDER BY username ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []ports.User
	for rows.Next() {
		var u ports.User
		var email sql.NullString
		var createdAt time.Time
		if err := rows.Scan(&u.Username, &email, &createdAt); err != nil {
			return nil, err
		}
		u.Email = email.String
		u.CreatedAt = createdAt
		users = append(users, u)
	}
	return users, rows.Err()
}

// DeleteUser deletes a local user by username.
func (s *Store) DeleteUser(ctx context.Context, username string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE username = ?`, username)
	if err != nil {
		return fmt.Errorf("delete user %q: %w", username, err)
	}
	return nil
}
