package domain

import "strings"

type ResourceTier string

const (
	TierSmall  ResourceTier = "small"  // 4 GiB
	TierMedium ResourceTier = "medium" // 8 GiB
	TierLarge  ResourceTier = "large"  // 12 GiB
)

func (t ResourceTier) MemoryGiB() int {
	switch t {
	case TierSmall:
		return 4
	case TierMedium:
		return 8
	case TierLarge:
		return 12
	default:
		return 8
	}
}

func (t ResourceTier) MemoryLimitGiB() int {
	switch t {
	case TierSmall:
		return 6
	case TierLarge:
		return 16
	default:
		return 10
	}
}

func (t ResourceTier) HeapInitMemoryGiB() int {
	switch t {
	case TierSmall:
		return 3
	case TierLarge:
		return 10
	default:
		return 6
	}
}

func NormalizeTier(t string) ResourceTier {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case string(TierSmall):
		return TierSmall
	case string(TierLarge):
		return TierLarge
	default:
		return TierMedium
	}
}
