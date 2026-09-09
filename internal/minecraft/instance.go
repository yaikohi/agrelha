package minecraft

import (
	"agrelha/internal/domain"
)

type ResourceTier = domain.ResourceTier

const (
	TierSmall  = domain.TierSmall  // 4 GiB
	TierMedium = domain.TierMedium // 8 GiB
	TierLarge  = domain.TierLarge  // 12 GiB
)

func NormalizeTier(t string) ResourceTier {
	return domain.NormalizeTier(t)
}

type InstanceState = domain.InstanceState

const (
	StateRunning      = domain.StateRunning
	StateStopped      = domain.StateStopped
	StateProvisioning = domain.StateProvisioning
	StateError        = domain.StateError
)

const (
	MaxInstances   = domain.DefaultMaxInstances
	MaxRunning     = domain.DefaultMaxRunning
	TotalBudgetGiB = domain.DefaultTotalBudgetGiB
)

type Instance = domain.Instance

// DefaultLBBaseIP is only a fallback; operators set their own LB range.
//
// NOTE: an LB IP is a Kubernetes networking concept — Docker binds a host port
// instead. Allocation belongs in the runtime adapter, not on the domain
// Instance. See docs/modularization-plan.md phase 4.
// Empty means "let the load balancer allocate an address", which is the right
// default for a cluster we know nothing about.
const DefaultLBBaseIP = ""

// AssignLBIP gives Instance N the Nth address after base.
func AssignLBIP(base string, number int) string {
	return domain.AssignLBIP(base, number)
}
