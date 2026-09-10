package instances

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

// AdoptLegacyValheim checks if a pre-existing singleton Valheim workload exists
// in the runtime and adopts it into slot #01 if repo does not have slot #01.
// If the legacy deployment does not exist in the runtime (e.g. running on local
// or in a clean cluster), no instance is adopted.
func AdoptLegacyValheim(
	ctx context.Context,
	repo ports.InstanceRepository,
	rt ports.Runtime,
	legacyRef ports.ServerRef,
	defaultLBIP string,
	serverName string,
) (*domain.Instance, error) {
	if repo == nil || rt == nil || legacyRef.Name == "" {
		return nil, nil
	}

	existing, err := repo.Get(1)
	if err != nil {
		return nil, fmt.Errorf("check slot 1 for legacy adoption: %w", err)
	}
	if existing != nil {
		return existing, nil
	}

	st, err := rt.Status(ctx, legacyRef)
	if err != nil || st.Lifecycle == ports.LifecycleUnknown {
		// Legacy workload does not exist in the runtime; nothing to adopt.
		return nil, nil
	}

	if serverName == "" {
		serverName = "Valheim"
	}
	slug := domain.Slugify(serverName)
	if slug == "" || slug == "default" {
		slug = "server"
	}

	initialState := domain.StateStopped
	if st.Available || st.Lifecycle == ports.LifecycleRunning {
		initialState = domain.StateRunning
	}

	inst := domain.Instance{
		GameID:     domain.GameValheim,
		Number:     1,
		Name:       serverName,
		Slug:       slug,
		Tier:       domain.TierMedium,
		State:      initialState,
		MaxPlayers: 10,
		LBIP:       defaultLBIP,
		MOTD:       fmt.Sprintf("%s (%s)", serverName, defaultLBIP),
		CreatedAt:  time.Now().UTC(),
		LastUsed:   time.Now().UTC(),
	}
	inst.EnsureDefaults(defaultLBIP)

	if err := repo.Upsert(inst); err != nil {
		return nil, fmt.Errorf("adopt legacy valheim instance into slot #1: %w", err)
	}

	slog.Info("adopted legacy valheim deployment into slot #01",
		"slot", 1,
		"name", inst.Name,
		"state", inst.State,
		"lb_ip", inst.LBIP,
	)

	return &inst, nil
}
