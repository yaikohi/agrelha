package valheim

import (
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/domain"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
)

// ValheimDashboard renders the Valheim instances management view.
func (h *Handler) ValheimDashboard(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		if h.cfg.LegacyConsole != nil {
			return h.cfg.LegacyConsole(c)
		}
		return c.Status(fiber.StatusServiceUnavailable).SendString("Valheim instance manager not configured")
	}

	instances, err := h.cfg.ValheimInstances.ListInstances(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("List Valheim instances failed: " + err.Error())
	}

	budget := h.cfg.ValheimInstances.Budget(instances)

	updateCounts := map[int]int{}
	if h.cfg.ModUpdates != nil {
		updateCounts = h.cfg.ModUpdates.Counts()
	}

	uiInstances := make([]pages.InstanceUI, 0, len(instances))
	for _, inst := range instances {
		canStart := true
		blockedReason := ""

		if inst.State == domain.StateRunning {
			canStart = false
		} else if budget.RunningCount >= budget.MaxRunning {
			canStart = false
			blockedReason = fmt.Sprintf("Max %d running instances reached", budget.MaxRunning)
		} else if budget.UsedGiB+inst.MemoryGiB() > budget.TotalBudgetGiB {
			canStart = false
			blockedReason = fmt.Sprintf("Exceeds %d GiB RAM budget (%d used + %d required)",
				budget.TotalBudgetGiB, budget.UsedGiB, inst.MemoryGiB())
		}

		uiInstances = append(uiInstances, pages.InstanceUI{
			GameID:             string(inst.GameID),
			Number:             inst.Number,
			Name:               inst.Name,
			Slug:               inst.Slug,
			Password:           inst.Password,
			Source:             string(inst.Source),
			Seed:               inst.Seed,
			Tier:               string(inst.Tier),
			MemoryGiB:          inst.MemoryGiB(),
			State:              string(inst.State),
			MOTD:               inst.MOTD,
			LBIP:               inst.LBIP,
			ModUpdates:         updateCounts[inst.Number],
			CanStart:           canStart,
			StartBlockedReason: blockedReason,
		})
	}

	budgetUI := pages.BudgetUI{
		UsedGiB:        budget.UsedGiB,
		TotalBudgetGiB: budget.TotalBudgetGiB,
		RunningCount:   budget.RunningCount,
		MaxRunning:     budget.MaxRunning,
		TotalInstances: budget.TotalInstances,
		MaxInstances:   budget.MaxInstances,
	}

	return shared.Render(c, pages.ValheimDashboard(uiInstances, budgetUI))
}

// ValheimInstanceStart starts a Valheim instance.
func (h *Handler) ValheimInstanceStart(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager not configured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	if err := h.cfg.ValheimInstances.StartInstance(c.UserContext(), num, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Start failed: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Starting Valheim instance #%02d...", num), nil)
}

// ValheimInstanceStop stops a Valheim instance.
func (h *Handler) ValheimInstanceStop(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager not configured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	if err := h.cfg.ValheimInstances.StopInstance(c.UserContext(), num, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Stop failed: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Stopping Valheim instance #%02d...", num), nil)
}

// ValheimInstanceRestart restarts a Valheim instance.
func (h *Handler) ValheimInstanceRestart(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager not configured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	if err := h.cfg.ValheimInstances.RestartInstance(c.UserContext(), num, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Restart failed: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Restarting Valheim instance #%02d...", num), nil)
}

// ValheimInstanceDelete deletes a Valheim instance.
func (h *Handler) ValheimInstanceDelete(c *fiber.Ctx) error {
	if h.cfg.ValheimInstances == nil {
		return shared.SSEToast(c, "err", "Valheim instance manager not configured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	if err := h.cfg.ValheimInstances.DeleteInstance(c.UserContext(), num, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Failed to delete world: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Deleted Valheim instance #%02d (final snapshot saved to backups).", num), nil)
}
