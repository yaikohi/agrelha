package authz

import (
	"context"
	"errors"
	"fmt"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type Service struct {
	accounts ports.Accounts
	sessions ports.Sessions
	registry ports.RoleRegistry
	audit    ports.AuditRecorder
	now      func() time.Time
}

type Option func(*Service)

func WithRoleRegistry(r ports.RoleRegistry) Option {
	return func(s *Service) { s.registry = r }
}

func WithAudit(a ports.AuditRecorder) Option {
	return func(s *Service) { s.audit = a }
}

func WithClock(fn func() time.Time) Option {
	return func(s *Service) { s.now = fn }
}

func New(accounts ports.Accounts, sessions ports.Sessions, opts ...Option) *Service {
	s := &Service{accounts: accounts, sessions: sessions, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *Service) HasRegistry() bool { return s != nil && s.registry != nil }

func (s *Service) RecordSignIn(ctx context.Context, id domain.Identity) error {
	if s == nil || s.accounts == nil {
		return nil
	}
	if id.Subject == "" {
		return errors.New("record sign-in: empty subject")
	}
	now := s.now()
	return s.accounts.UpsertAccount(ctx, domain.Account{
		Subject:   id.Subject,
		Email:     id.Email,
		Name:      id.Name,
		FirstSeen: now,
		LastSeen:  now,
	})
}

func (s *Service) ListAccounts(ctx context.Context) ([]domain.Account, error) {
	if s == nil || s.accounts == nil {
		return nil, nil
	}
	return s.accounts.ListAccounts(ctx)
}

func (s *Service) Account(ctx context.Context, subject string) (*domain.Account, error) {
	if s == nil || s.accounts == nil {
		return nil, nil
	}
	return s.accounts.GetAccount(ctx, subject)
}

func (s *Service) RolesFor(ctx context.Context, subject string) ([]domain.Role, error) {
	if !s.HasRegistry() {
		return nil, nil
	}
	return s.registry.RolesFor(ctx, subject)
}

func (s *Service) requireAccount(ctx context.Context, subject string) error {
	if s.accounts == nil {
		return ports.ErrNoAccount
	}
	a, err := s.accounts.GetAccount(ctx, subject)
	if err != nil {
		return err
	}
	if a == nil {
		return ports.ErrNoAccount
	}
	return nil
}

func (s *Service) Grant(ctx context.Context, subject string, game domain.GameID, number int, actor string) error {
	if !s.HasRegistry() {
		return errors.New("grant: no role registry configured")
	}
	if err := s.requireAccount(ctx, subject); err != nil {
		return err
	}
	role := domain.InstanceRole(game, number)
	if err := s.registry.GrantRole(ctx, subject, role); err != nil {
		return fmt.Errorf("grant %s to %s: %w", role, subject, err)
	}
	s.record(actor, "grant", fmt.Sprintf("%s -> %s", role, subject))
	return nil
}

func (s *Service) Revoke(ctx context.Context, subject string, game domain.GameID, number int, actor string) error {
	if !s.HasRegistry() {
		return errors.New("revoke: no role registry configured")
	}
	role := domain.InstanceRole(game, number)
	if err := s.registry.RevokeRole(ctx, subject, role); err != nil {
		return fmt.Errorf("revoke %s from %s: %w", role, subject, err)
	}
	s.record(actor, "revoke", fmt.Sprintf("%s -> %s", role, subject))
	return nil
}

func (s *Service) RevokeSessions(ctx context.Context, subject, actor string) error {
	if s == nil || s.sessions == nil {
		return nil
	}
	if err := s.sessions.DeleteSessionsFor(ctx, subject); err != nil {
		return err
	}
	s.record(actor, "revoke-sessions", subject)
	return nil
}

func (s *Service) EnsureInstanceRole(ctx context.Context, inst domain.Instance) error {
	if !s.HasRegistry() {
		return nil
	}
	role := domain.InstanceRole(inst.GameID, inst.Number)
	display := fmt.Sprintf("%s %02d - %s", inst.GameID, inst.Number, inst.Name)
	return s.registry.AddRole(ctx, role, display)
}

func (s *Service) RetireInstanceRole(ctx context.Context, inst domain.Instance) error {
	if !s.HasRegistry() {
		return nil
	}
	return s.registry.RemoveRole(ctx, domain.InstanceRole(inst.GameID, inst.Number))
}

func (s *Service) PurgeExpiredSessions(ctx context.Context) (int, error) {
	if s == nil || s.sessions == nil {
		return 0, nil
	}
	return s.sessions.PurgeExpiredSessions(ctx, s.now())
}

func (s *Service) record(actor, action, detail string) {
	if s.audit == nil {
		return
	}
	if actor == "" {
		actor = "-"
	}
	_ = s.audit.RecordAudit(actor, action, detail)
}

func (s *Service) ReconcileInstanceRoles(ctx context.Context, insts []domain.Instance) (int, int) {
	if !s.HasRegistry() {
		return 0, 0
	}
	var created, failed int
	for _, inst := range insts {
		err := s.EnsureInstanceRole(ctx, inst)
		switch {
		case err == nil:
			created++
		case errors.Is(err, ports.ErrRoleExists):
		default:
			failed++
		}
	}
	return created, failed
}
