package ports

import (
	"context"
	"errors"
	"time"

	"agrelha/internal/domain"
)

var (
	ErrRoleExists   = errors.New("role already exists")
	ErrRoleNotFound = errors.New("role not found")
	ErrNoAccount    = errors.New("account has never signed in")
)

type Accounts interface {
	UpsertAccount(ctx context.Context, a domain.Account) error
	GetAccount(ctx context.Context, subject string) (*domain.Account, error)
	ListAccounts(ctx context.Context) ([]domain.Account, error)
}

type Session struct {
	ID        string
	Subject   string
	Email     string
	Name      string
	Roles     []domain.Role
	IDToken   string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type Sessions interface {
	CreateSession(ctx context.Context, s Session) error
	LoadSession(ctx context.Context, id string) (*Session, error)
	TouchSession(ctx context.Context, id string, expiresAt time.Time) error
	DeleteSession(ctx context.Context, id string) error
	DeleteSessionsFor(ctx context.Context, subject string) error
	PurgeExpiredSessions(ctx context.Context, now time.Time) (int, error)
}

type RoleRegistry interface {
	AddRole(ctx context.Context, role domain.Role, displayName string) error
	RemoveRole(ctx context.Context, role domain.Role) error
	GrantRole(ctx context.Context, subject string, role domain.Role) error
	RevokeRole(ctx context.Context, subject string, role domain.Role) error
	RolesFor(ctx context.Context, subject string) ([]domain.Role, error)
}
