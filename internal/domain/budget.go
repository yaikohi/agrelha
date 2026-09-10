package domain

import "fmt"

const (
	DefaultMaxInstances   = 4
	DefaultMaxRunning     = 2
	DefaultTotalBudgetGiB = 24

	DefaultValheimMaxInstances   = 4
	DefaultValheimMaxRunning     = 2
	DefaultValheimTotalBudgetGiB = 16
)

// Budget represents the global resource budget across all games.
type Budget struct {
	TotalBudgetGiB int
	UsedGiB        int
	MaxRunning     int
	RunningCount   int
	MaxInstances   int
	TotalInstances int
}

// CalculateBudget computes memory and running server counts across instances.
func CalculateBudget(instances []Instance, totalBudgetGiB, maxRunning, maxInstances int) Budget {
	if totalBudgetGiB <= 0 {
		totalBudgetGiB = DefaultTotalBudgetGiB
	}
	if maxRunning <= 0 {
		maxRunning = DefaultMaxRunning
	}
	if maxInstances <= 0 {
		maxInstances = DefaultMaxInstances
	}

	usedGiB := 0
	runningCount := 0
	for _, inst := range instances {
		if inst.State == StateRunning {
			runningCount++
			usedGiB += inst.MemoryGiB()
		}
	}

	return Budget{
		TotalBudgetGiB: totalBudgetGiB,
		UsedGiB:        usedGiB,
		MaxRunning:     maxRunning,
		RunningCount:   runningCount,
		MaxInstances:   maxInstances,
		TotalInstances: len(instances),
	}
}

// AddUsage records additional running workloads (e.g. standalone game servers like Valheim).
func (b *Budget) AddUsage(runningCount, usedGiB int) {
	b.RunningCount += runningCount
	b.UsedGiB += usedGiB
}

// CanStart checks whether inst can be started within this budget.
func (b Budget) CanStart(inst Instance) error {
	if b.MaxRunning > 0 && b.RunningCount >= b.MaxRunning {
		return fmt.Errorf("cannot start instance: maximum of %d running instances reached (please stop another server first)", b.MaxRunning)
	}
	if b.TotalBudgetGiB > 0 && b.UsedGiB+inst.MemoryGiB() > b.TotalBudgetGiB {
		return fmt.Errorf("cannot start instance: RAM budget exceeded (%d GiB in use, requires %d GiB, total budget is %d GiB)",
			b.UsedGiB, inst.MemoryGiB(), b.TotalBudgetGiB)
	}
	return nil
}

// CanCreate checks whether a new instance can be created within max instance limits.
func (b Budget) CanCreate() error {
	if b.MaxInstances > 0 && b.TotalInstances >= b.MaxInstances {
		return fmt.Errorf("cannot create instance: maximum limit of %d instances reached", b.MaxInstances)
	}
	return nil
}
