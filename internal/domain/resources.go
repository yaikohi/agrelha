package domain

import (
	"fmt"
	"strings"
)

type Resources struct {
	MemRequestGiB   int
	MemLimitGiB     int
	CPURequestMilli int
	CPULimitMilli   int
}

func (r Resources) IsZero() bool {
	return r.MemRequestGiB == 0 && r.MemLimitGiB == 0 &&
		r.CPURequestMilli == 0 && r.CPULimitMilli == 0
}

func (r Resources) Validate() error {
	if r.MemRequestGiB <= 0 {
		return fmt.Errorf("memory request must be positive, got %d", r.MemRequestGiB)
	}
	if r.MemLimitGiB < r.MemRequestGiB {
		return fmt.Errorf("memory limit %d is below request %d", r.MemLimitGiB, r.MemRequestGiB)
	}
	if r.CPURequestMilli < 0 || r.CPULimitMilli < 0 {
		return fmt.Errorf("cpu must not be negative")
	}
	if r.CPULimitMilli > 0 && r.CPULimitMilli < r.CPURequestMilli {
		return fmt.Errorf("cpu limit %d is below request %d", r.CPULimitMilli, r.CPURequestMilli)
	}
	return nil
}

func (r Resources) Fits(ceiling Resources) bool {
	if ceiling.MemLimitGiB > 0 && r.MemLimitGiB > ceiling.MemLimitGiB {
		return false
	}
	if ceiling.MemRequestGiB > 0 && r.MemRequestGiB > ceiling.MemRequestGiB {
		return false
	}
	if ceiling.CPULimitMilli > 0 && r.CPULimitMilli > ceiling.CPULimitMilli {
		return false
	}
	return true
}

const legacyCPURequestMilli = 2000

type Tier struct {
	Key         string
	GameID      GameID
	Name        string
	Resources   Resources
	HeapInitGiB int
	SortOrder   int
}

func LegacyResources(gameID GameID, tier ResourceTier) Resources {
	switch gameID {
	case GameMinecraft:
		return Resources{
			MemRequestGiB:   tier.MemoryGiB(),
			MemLimitGiB:     tier.MemoryLimitGiB(),
			CPURequestMilli: legacyCPURequestMilli,
		}
	case GameValheim:
		switch tier {
		case TierSmall:
			return Resources{MemRequestGiB: 4, MemLimitGiB: 5, CPURequestMilli: legacyCPURequestMilli}
		case TierLarge:
			return Resources{MemRequestGiB: 8, MemLimitGiB: 10, CPURequestMilli: legacyCPURequestMilli}
		default:
			return Resources{MemRequestGiB: 6, MemLimitGiB: 7, CPURequestMilli: legacyCPURequestMilli}
		}
	default:
		return Resources{}
	}
}

// gmodTierResources are built-in defaults, not history: Garry's Mod has no
// existing worlds to preserve. Source servers are far lighter than modded
// Minecraft, so the whole catalogue sits below Valheim's smallest tier.
var gmodTierResources = map[ResourceTier]Resources{
	TierSmall:  {MemRequestGiB: 2, MemLimitGiB: 3, CPURequestMilli: legacyCPURequestMilli},
	TierMedium: {MemRequestGiB: 4, MemLimitGiB: 6, CPURequestMilli: legacyCPURequestMilli},
	TierLarge:  {MemRequestGiB: 8, MemLimitGiB: 10, CPURequestMilli: legacyCPURequestMilli},
}

// BuiltinResources is what a game+tier means when an Instance carries no
// resources of its own. For Minecraft and Valheim that is strictly the
// historical table, which must never change; for newer games it is simply the
// shipped default.
func BuiltinResources(gameID GameID, tier ResourceTier) Resources {
	if gameID == GameGMod {
		if r, ok := gmodTierResources[tier]; ok {
			return r
		}
		return gmodTierResources[TierMedium]
	}
	return LegacyResources(gameID, tier)
}

func LegacyHeapInitGiB(gameID GameID, tier ResourceTier) int {
	if gameID != GameMinecraft {
		return 0
	}
	return tier.HeapInitMemoryGiB()
}

type GameSettings struct {
	GameID         GameID
	TotalBudgetGiB int
	MaxInstances   int
	MaxRunning     int
	Ceiling        Resources
}

func (g GameSettings) Allows(r Resources) error {
	if !r.Fits(g.Ceiling) {
		return fmt.Errorf("requested %d GiB exceeds the %s ceiling of %d GiB",
			r.MemLimitGiB, g.GameID, g.Ceiling.MemLimitGiB)
	}
	return nil
}

func DefaultTiersFor(gameID GameID) []Tier {
	var out []Tier
	for i, key := range []ResourceTier{TierSmall, TierMedium, TierLarge} {
		r := BuiltinResources(gameID, key)
		if r.IsZero() {
			continue
		}
		out = append(out, Tier{
			Key:         string(key),
			GameID:      gameID,
			Name:        strings.ToUpper(string(key)[:1]) + string(key)[1:],
			Resources:   r,
			HeapInitGiB: LegacyHeapInitGiB(gameID, key),
			SortOrder:   i,
		})
	}
	return out
}
