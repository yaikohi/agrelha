package capacity

import (
	"context"
	"errors"
	"fmt"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

var ErrAboveCeiling = errors.New("resources exceed the ceiling for this game")

type Service struct {
	tiers    ports.Tiers
	settings ports.GameSettingsStore
	globals  ports.GlobalSettings
	audit    ports.AuditRecorder
}

type Option func(*Service)

func WithAudit(a ports.AuditRecorder) Option { return func(s *Service) { s.audit = a } }

func WithGlobals(g ports.GlobalSettings) Option { return func(s *Service) { s.globals = g } }

func New(tiers ports.Tiers, settings ports.GameSettingsStore, opts ...Option) *Service {
	s := &Service{tiers: tiers, settings: settings}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Seed writes the starting catalogue and settings for a game the first time it
// is seen. Existing rows are never overwritten: once an operator has edited a
// tier, the compiled-in defaults stop being the authority.
func (s *Service) Seed(ctx context.Context, gameID domain.GameID, defaults domain.GameSettings) error {
	if s == nil {
		return nil
	}
	if s.settings != nil {
		existing, err := s.settings.GetGameSettings(ctx, gameID)
		if err != nil {
			return err
		}
		if existing == nil {
			defaults.GameID = gameID
			if err := s.settings.PutGameSettings(ctx, defaults); err != nil {
				return err
			}
		}
	}
	if s.tiers == nil {
		return nil
	}
	have, err := s.tiers.ListTiers(ctx, gameID)
	if err != nil {
		return err
	}
	if len(have) > 0 {
		return nil
	}
	for _, t := range domain.DefaultTiersFor(gameID) {
		if err := s.tiers.PutTier(ctx, t); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Tiers(ctx context.Context, gameID domain.GameID) ([]domain.Tier, error) {
	if s == nil || s.tiers == nil {
		return nil, nil
	}
	return s.tiers.ListTiers(ctx, gameID)
}

func (s *Service) Tier(ctx context.Context, gameID domain.GameID, key string) (*domain.Tier, error) {
	list, err := s.Tiers(ctx, gameID)
	if err != nil {
		return nil, err
	}
	for _, t := range list {
		if t.Key == key {
			return &t, nil
		}
	}
	return nil, nil
}

func (s *Service) PutTier(ctx context.Context, t domain.Tier, actor string) error {
	if s == nil || s.tiers == nil {
		return errors.New("no tier store configured")
	}
	if err := t.Resources.Validate(); err != nil {
		return fmt.Errorf("tier %s/%s: %w", t.GameID, t.Key, err)
	}
	if err := s.tiers.PutTier(ctx, t); err != nil {
		return err
	}
	s.record(actor, "tier-save", fmt.Sprintf("%s/%s -> %d/%d GiB",
		t.GameID, t.Key, t.Resources.MemRequestGiB, t.Resources.MemLimitGiB))
	return nil
}

func (s *Service) DeleteTier(ctx context.Context, gameID domain.GameID, key, actor string) error {
	if s == nil || s.tiers == nil {
		return errors.New("no tier store configured")
	}
	if err := s.tiers.DeleteTier(ctx, gameID, key); err != nil {
		return err
	}
	s.record(actor, "tier-delete", fmt.Sprintf("%s/%s", gameID, key))
	return nil
}

func (s *Service) Settings(ctx context.Context, gameID domain.GameID) (domain.GameSettings, error) {
	if s == nil || s.settings == nil {
		return domain.GameSettings{GameID: gameID}, nil
	}
	got, err := s.settings.GetGameSettings(ctx, gameID)
	if err != nil {
		return domain.GameSettings{GameID: gameID}, err
	}
	if got == nil {
		return domain.GameSettings{GameID: gameID}, nil
	}
	return *got, nil
}

func (s *Service) PutSettings(ctx context.Context, g domain.GameSettings, actor string) error {
	if s == nil || s.settings == nil {
		return errors.New("no settings store configured")
	}
	if err := s.settings.PutGameSettings(ctx, g); err != nil {
		return err
	}
	s.record(actor, "game-settings-save", fmt.Sprintf("%s budget=%d max=%d running=%d ceiling=%d",
		g.GameID, g.TotalBudgetGiB, g.MaxInstances, g.MaxRunning, g.Ceiling.MemLimitGiB))
	return nil
}

// CheckResources is the single gate for "may this account run a world this
// size". Admins are not bound by the ceiling; everyone else is.
func (s *Service) CheckResources(ctx context.Context, gameID domain.GameID, r domain.Resources, isAdmin bool) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if isAdmin {
		return nil
	}
	g, err := s.Settings(ctx, gameID)
	if err != nil {
		return err
	}
	if err := g.Allows(r); err != nil {
		return fmt.Errorf("%w: %s", ErrAboveCeiling, err)
	}
	return nil
}

func (s *Service) record(actor, action, detail string) {
	if s == nil || s.audit == nil {
		return
	}
	if actor == "" {
		actor = "-"
	}
	_ = s.audit.RecordAudit(actor, action, detail)
}

const (
	// FreeCreationsKey is a global total across every game, not a per-game
	// limit: an Account holding one world of any game has used its allowance.
	FreeCreationsKey     = "free_creations"
	DefaultFreeCreations = 1
)

func (s *Service) FreeCreations(ctx context.Context) int {
	if s == nil || s.globals == nil {
		return DefaultFreeCreations
	}
	n, ok, err := s.globals.GetGlobalInt(ctx, FreeCreationsKey)
	if err != nil || !ok || n < 0 {
		return DefaultFreeCreations
	}
	return n
}

func (s *Service) SetFreeCreations(ctx context.Context, n int, actor string) error {
	if s == nil || s.globals == nil {
		return errors.New("no global settings store configured")
	}
	if n < 0 {
		return fmt.Errorf("creation allowance cannot be negative, got %d", n)
	}
	if err := s.globals.SetGlobalInt(ctx, FreeCreationsKey, n); err != nil {
		return err
	}
	s.record(actor, "free-creations-set", fmt.Sprintf("%d", n))
	return nil
}
