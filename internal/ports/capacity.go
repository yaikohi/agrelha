package ports

import (
	"context"

	"agrelha/internal/domain"
)

type Tiers interface {
	ListTiers(ctx context.Context, gameID domain.GameID) ([]domain.Tier, error)
	PutTier(ctx context.Context, t domain.Tier) error
	DeleteTier(ctx context.Context, gameID domain.GameID, key string) error
}

type GameSettingsStore interface {
	GetGameSettings(ctx context.Context, gameID domain.GameID) (*domain.GameSettings, error)
	PutGameSettings(ctx context.Context, s domain.GameSettings) error
}

type GlobalSettings interface {
	GetGlobalInt(ctx context.Context, key string) (int, bool, error)
	SetGlobalInt(ctx context.Context, key string, value int) error
}
