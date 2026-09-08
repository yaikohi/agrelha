package server

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/cmd/web/pages"
	"agrelha/internal/minecraft"
)

func (s *FiberServer) mcDashboard(c *fiber.Ctx) error {
	if s.mcInstances == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Instance manager not configured")
	}

	instances, err := s.mcInstances.ListInstances(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("List instances failed: " + err.Error())
	}

	budget := s.mcInstances.Budget(instances)

	uiInstances := make([]pages.InstanceUI, 0, len(instances))
	for _, inst := range instances {
		canStart := true
		blockedReason := ""

		if inst.State == minecraft.StateRunning {
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

		uiInstances = append(uiInstances, pages.InstanceUI{
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

	return render(c, pages.MinecraftDashboard(uiInstances, budgetUI))
}

func (s *FiberServer) mcInstanceCreate(c *fiber.Ctx) error {
	if s.mcInstances == nil {
		return sseToast(c, "err", "Instance manager not configured.", nil)
	}

	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return sseToast(c, "err", "World name is required.", nil)
	}

	loaderStr := strings.ToLower(strings.TrimSpace(c.FormValue("loader")))
	mcVersion := strings.TrimSpace(c.FormValue("mc_version"))
	if mcVersion == "" {
		mcVersion = "1.21.1"
	}
	tier := minecraft.NormalizeTier(c.FormValue("tier"))
	seed := strings.TrimSpace(c.FormValue("seed"))
	mods := c.FormValue("mods")

	inst := minecraft.Instance{
		Name:      name,
		Seed:      seed,
		MCVersion: mcVersion,
		Tier:      tier,
		State:     minecraft.StateRunning,
	}

	if loaderStr == "vanilla" {
		inst.Source = minecraft.SourceVanilla
		inst.Loader = ""
	} else {
		inst.Source = minecraft.SourceModlist
		inst.Loader = minecraft.NormalizeLoader(loaderStr)
	}

	created, err := s.mcInstances.CreateInstance(c.UserContext(), inst, mods)
	if err != nil {
		return sseToast(c, "err", "Failed to create world: "+err.Error(), nil)
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-instance-create", fmt.Sprintf("World #%02d %q", created.Number, created.Name))
	_ = s.store.RecordEvent("mc-instance-create", s.actor(c))

	return sseToast(c, "ok", fmt.Sprintf("World %q created as instance #%02d. ArgoCD will sync manifests.", created.Name, created.Number), nil)
}

func (s *FiberServer) mcInstanceStart(c *fiber.Ctx) error {
	if s.mcInstances == nil {
		return sseToast(c, "err", "Instance manager not configured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return sseToast(c, "err", "Invalid instance number.", nil)
	}

	if err := s.mcInstances.StartInstance(c.UserContext(), num); err != nil {
		return sseToast(c, "err", "Failed to start world: "+err.Error(), nil)
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-instance-start", fmt.Sprintf("Instance #%02d", num))
	_ = s.store.RecordEvent("mc-instance-start", s.actor(c))

	return sseToast(c, "ok", fmt.Sprintf("Starting instance #%02d...", num), nil)
}

func (s *FiberServer) mcInstanceStop(c *fiber.Ctx) error {
	if s.mcInstances == nil {
		return sseToast(c, "err", "Instance manager not configured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return sseToast(c, "err", "Invalid instance number.", nil)
	}

	inst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err == nil && inst != nil {
		// 1. RCON save-all flush if active (with 250ms deadline so unreachable RCON does not hang)
		if s.mcRconPool != nil && inst.State == minecraft.StateRunning {
			addr := fmt.Sprintf("%s.minecraft-modded.svc.cluster.local:25575", inst.ServiceName())
			client := s.mcRconPool.ClientFor(addr)
			done := make(chan struct{})
			go func() {
				_, _ = client.Execute("/save-all flush")
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(250 * time.Millisecond):
			}
		}

		// 2. Pre-stop auto-backup
		if s.mck8s != nil {
			jobName := fmt.Sprintf("mc-backup-%s-%02d-stop-%d", inst.Slug, inst.Number, time.Now().Unix())
			archiveName := minecraft.FormatBackupFileName(inst.Slug, inst.Number, "stop")
			_ = s.mck8s.CreateBackupJob(c.UserContext(), jobName, archiveName, inst.PVCName(), "minecraft-modded-backups")
		}

		// 3. Prune older backups
		if s.cfg != nil && s.cfg.BackupsDir != "" {
			_ = minecraft.PruneBackups(s.cfg.BackupsDir, inst.Slug, inst.Number, 5)
		}
	}

	if err := s.mcInstances.StopInstance(c.UserContext(), num); err != nil {
		return sseToast(c, "err", "Failed to stop world: "+err.Error(), nil)
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-instance-stop", fmt.Sprintf("Instance #%02d", num))
	_ = s.store.RecordEvent("mc-instance-stop", s.actor(c))

	return sseToast(c, "ok", fmt.Sprintf("Stopping instance #%02d (pre-stop snapshot initiated)...", num), nil)
}

func (s *FiberServer) mcInstanceDelete(c *fiber.Ctx) error {
	if s.mcInstances == nil {
		return sseToast(c, "err", "Instance manager not configured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return sseToast(c, "err", "Invalid instance number.", nil)
	}

	inst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err == nil && inst != nil && s.mck8s != nil {
		// Pre-delete final snapshot
		jobName := fmt.Sprintf("mc-backup-%s-%02d-final-%d", inst.Slug, inst.Number, time.Now().Unix())
		archiveName := minecraft.FormatBackupFileName(inst.Slug, inst.Number, "final")
		_ = s.mck8s.CreateBackupJob(c.UserContext(), jobName, archiveName, inst.PVCName(), "minecraft-modded-backups")
	}

	if err := s.mcInstances.DeleteInstance(c.UserContext(), num); err != nil {
		return sseToast(c, "err", "Failed to delete world: "+err.Error(), nil)
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-instance-delete", fmt.Sprintf("Instance #%02d", num))
	_ = s.store.RecordEvent("mc-instance-delete", s.actor(c))

	return sseToast(c, "ok", fmt.Sprintf("Deleted instance #%02d (final snapshot saved to backups).", num), nil)
}

func (s *FiberServer) mcInstanceRestart(c *fiber.Ctx) error {
	if s.mcInstances == nil {
		return sseToast(c, "err", "Instance manager not configured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return sseToast(c, "err", "Invalid instance number.", nil)
	}

	inst, err := s.mcInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return sseToast(c, "err", "Instance not found.", nil)
	}

	if s.mck8s != nil {
		if err := s.mck8s.RestartDeployment(c.UserContext(), inst.DeploymentName()); err != nil {
			return sseToast(c, "err", "Restart failed: "+err.Error(), nil)
		}
	}

	_ = s.store.RecordAudit(s.actor(c), "mc-instance-restart", fmt.Sprintf("Instance #%02d", num))
	_ = s.store.RecordEvent("mc-instance-restart", s.actor(c))

	return sseToast(c, "ok", fmt.Sprintf("Restarting instance #%02d...", num), nil)
}
