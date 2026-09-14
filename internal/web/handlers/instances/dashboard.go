package instances

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/domain"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
)

// MCDashboard renders the Minecraft instances management view.
func (h *Handler) MCDashboard(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Instance manager not configured")
	}

	instances, err := h.cfg.MCInstances.ListInstances(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("List instances failed: " + err.Error())
	}

	budget := h.cfg.MCInstances.Budget(instances)

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

		packName := ""
		packRef := ""
		packProvider := ""
		if inst.Pack != nil {
			packName = inst.Pack.Name
			packRef = inst.Pack.Ref
			packProvider = string(inst.Pack.Provider)
		}

		updateCount := 0
		if h.cfg.ModUpdates != nil {
			updateCount = h.cfg.ModUpdates.Count(inst.Number)
		}

		uiInstances = append(uiInstances, pages.InstanceUI{
			GameID:             string(domain.GameMinecraft),
			Number:             inst.Number,
			Name:               inst.Name,
			Slug:               inst.Slug,
			Seed:               inst.Seed,
			Loader:             string(inst.Loader),
			Source:             string(inst.Source),
			Pack:               packName,
			PackRef:            packRef,
			PackProvider:       packProvider,
			MCVersion:          inst.MCVersion,
			Tier:               string(inst.Tier),
			MemoryGiB:          inst.MemoryGiB(),
			State:              string(inst.State),
			MOTD:               inst.MOTD,
			LBIP:               inst.LBIP,
			ModUpdates:         updateCount,
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

	return shared.Render(c, pages.MinecraftDashboard(uiInstances, budgetUI))
}

// MCInstanceCreate creates a new world directly without the wizard.
func (h *Handler) MCInstanceCreate(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return shared.SSEToast(c, "err", "Instance manager not configured.", nil)
	}

	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return shared.SSEToast(c, "err", "World name is required.", nil)
	}

	loaderStr := strings.ToLower(strings.TrimSpace(c.FormValue("loader")))
	mcVersion := strings.TrimSpace(c.FormValue("mc_version"))
	if mcVersion == "" {
		mcVersion = "1.21.1"
	}
	tier := domain.NormalizeTier(c.FormValue("tier"))
	seed := strings.TrimSpace(c.FormValue("seed"))
	mods := c.FormValue("mods")

	inst := domain.Instance{
		Name:      name,
		Seed:      seed,
		MCVersion: mcVersion,
		Tier:      tier,
		State:     domain.StateRunning,
	}

	if loaderStr == "vanilla" {
		inst.Source = domain.SourceVanilla
		inst.Loader = ""
	} else {
		inst.Source = domain.SourceModlist
		inst.Loader = domain.NormalizeLoader(loaderStr)
	}

	created, err := h.cfg.MCInstances.CreateInstance(c.UserContext(), inst, mods, h.cfg.Actor(c))
	if err != nil {
		return shared.SSEToast(c, "err", "Failed to create world: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("World %q created as instance #%02d. ArgoCD will sync manifests.", created.Name, created.Number), nil)
}

// MCInstanceStart starts a stopped Minecraft instance.
func (h *Handler) MCInstanceStart(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return shared.SSEToast(c, "err", "Instance manager not configured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	if err := h.cfg.MCInstances.StartInstance(c.UserContext(), num, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Failed to start world: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Starting instance #%02d...", num), nil)
}

// MCInstanceStop stops a running Minecraft instance after flushing and auto-backing up.
func (h *Handler) MCInstanceStop(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return shared.SSEToast(c, "err", "Instance manager not configured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	if err := h.cfg.MCInstances.StopInstance(c.UserContext(), num, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Failed to stop world: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Stopping instance #%02d (pre-stop snapshot initiated)...", num), nil)
}

// MCInstanceDelete deletes a Minecraft instance after creating a final snapshot.
func (h *Handler) MCInstanceDelete(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return shared.SSEToast(c, "err", "Instance manager not configured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	if err := h.cfg.MCInstances.DeleteInstance(c.UserContext(), num, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Failed to delete world: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Deleted instance #%02d (final snapshot saved to backups).", num), nil)
}

// MCInstanceRestart restarts a Minecraft instance.
func (h *Handler) MCInstanceRestart(c *fiber.Ctx) error {
	if h.cfg.MCInstances == nil {
		return shared.SSEToast(c, "err", "Instance manager not configured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	if err := h.cfg.MCInstances.RestartInstance(c.UserContext(), num, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Restart failed: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Restarting instance #%02d...", num), nil)
}
