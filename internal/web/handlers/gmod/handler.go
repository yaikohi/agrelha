package gmod

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/instances"
	"agrelha/internal/app/requests"
	"agrelha/internal/domain"
	"agrelha/internal/ports"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
)

type Config struct {
	Instances *instances.InstanceManager
	Workshop  ports.WorkshopResolver
	Requests  *requests.Service
	Actor     func(*fiber.Ctx) string
}

type Handler struct {
	cfg Config
}

func New(cfg Config) *Handler {
	if cfg.Actor == nil {
		cfg.Actor = shared.Actor
	}
	return &Handler{cfg: cfg}
}

func (h *Handler) RegisterProtected(router fiber.Router) {
	router.Get("/gmod", h.Dashboard)
	router.Get("/gmod/access", func(c *fiber.Ctx) error {
		return c.Redirect("/admins?game=gmod", fiber.StatusTemporaryRedirect)
	})
	router.Get("/gmod/create", h.WizardPage)
	router.Post("/api/gmod/wizard/create", h.WizardCreate)

	router.Get("/gmod/:num<int>", func(c *fiber.Ctx) error {
		return c.Redirect(fmt.Sprintf("/gmod/%s/overview", c.Params("num")))
	})
	router.Get("/gmod/:num<int>/:tab", h.InstancePage)

	router.Post("/api/gmod/instances/:num/start", h.InstanceStart)
	router.Post("/api/gmod/instances/:num/stop", h.InstanceStop)
	router.Post("/api/gmod/instances/:num/restart", h.InstanceRestart)
	router.Delete("/api/gmod/instances/:num", h.InstanceDelete)
}

func (h *Handler) Dashboard(c *fiber.Ctx) error {
	if h.cfg.Instances == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Garry's Mod instance manager not configured")
	}

	insts, err := h.cfg.Instances.ListInstances(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("List Garry's Mod instances failed: " + err.Error())
	}

	budget := h.cfg.Instances.Budget(insts)

	uiInstances := make([]pages.InstanceUI, 0, len(insts))
	for _, inst := range insts {
		canStart := true
		blockedReason := ""

		if inst.State == domain.StateRunning {
			canStart = false
		} else if budget.RunningCount >= budget.MaxRunning {
			canStart = false
			blockedReason = fmt.Sprintf("Max %d running instances reached", budget.MaxRunning)
		} else if budget.UsedGiB+inst.MemoryGiB(domain.GModProfile) > budget.TotalBudgetGiB {
			canStart = false
			blockedReason = fmt.Sprintf("Exceeds %d GiB RAM budget (%d used + %d required)",
				budget.TotalBudgetGiB, budget.UsedGiB, inst.MemoryGiB(domain.GModProfile))
		}

		pass := ""
		gamemode := ""
		mapName := ""
		collectionID := ""
		if inst.GMod != nil {
			pass = inst.GMod.Password
			gamemode = inst.GMod.Gamemode
			mapName = inst.GMod.Map
			collectionID = inst.GMod.CollectionID()
		}

		lbIP := inst.LBIP
		if lbIP == "" && h.cfg.Instances != nil {
			if st, err := h.cfg.Instances.RuntimeStatus(c.UserContext(), inst.Number); err == nil && st.Address != "" {
				lbIP = st.Address
			}
		}

		uiInstances = append(uiInstances, pages.InstanceUI{
			GameID:             string(inst.GameID),
			Number:             inst.Number,
			Name:               inst.Name,
			Slug:               inst.Slug,
			Password:           pass,
			Gamemode:           gamemode,
			Map:                mapName,
			CollectionID:       collectionID,
			Source:             string(inst.Source),
			Tier:               string(inst.Tier),
			MemoryGiB:          inst.MemoryGiB(domain.GModProfile),
			State:              string(inst.State),
			MOTD:               inst.MOTD,
			LBIP:               lbIP,
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

	return shared.Render(c, pages.GModDashboard(uiInstances, budgetUI))
}

func (h *Handler) WizardPage(c *fiber.Ctx) error {
	if h.cfg.Instances == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Garry's Mod instance manager not configured")
	}

	insts, err := h.cfg.Instances.ListInstances(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("List instances failed: " + err.Error())
	}

	budget := h.cfg.Instances.Budget(insts)
	budgetUI := pages.BudgetUI{
		UsedGiB:        budget.UsedGiB,
		TotalBudgetGiB: budget.TotalBudgetGiB,
		RunningCount:   budget.RunningCount,
		MaxRunning:     budget.MaxRunning,
		TotalInstances: budget.TotalInstances,
		MaxInstances:   budget.MaxInstances,
	}

	return shared.Render(c, pages.GModWizard(budgetUI))
}

func (h *Handler) WizardCreate(c *fiber.Ctx) error {
	if h.cfg.Instances == nil {
		return shared.SSEToast(c, "err", "Garry's Mod instance manager unconfigured.", nil)
	}

	var req struct {
		Name         string `json:"name" form:"name"`
		Password     string `json:"password" form:"password"`
		Gamemode     string `json:"gamemode" form:"gamemode"`
		Map          string `json:"map" form:"map"`
		CollectionID string `json:"collectionID" form:"collectionID"`
		Tier         string `json:"tier" form:"tier"`
	}
	_ = c.BodyParser(&req)

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.TrimSpace(c.FormValue("name"))
	}
	if name == "" {
		return shared.SSEToast(c, "err", "World name is required.", nil)
	}

	password := strings.TrimSpace(req.Password)
	if password == "" {
		password = strings.TrimSpace(c.FormValue("password"))
	}

	gamemode := strings.TrimSpace(req.Gamemode)
	if gamemode == "" {
		gamemode = strings.TrimSpace(c.FormValue("gamemode"))
	}
	if gamemode == "" {
		gamemode = "sandbox"
	}

	mapName := strings.TrimSpace(req.Map)
	if mapName == "" {
		mapName = strings.TrimSpace(c.FormValue("map"))
	}
	if mapName == "" {
		mapName = "gm_construct"
	}

	collectionID := strings.TrimSpace(req.CollectionID)
	if collectionID == "" {
		collectionID = strings.TrimSpace(c.FormValue("collectionID"))
	}

	tierStr := strings.TrimSpace(req.Tier)
	if tierStr == "" {
		tierStr = strings.TrimSpace(c.FormValue("tier"))
	}
	tier := domain.NormalizeTier(tierStr)

	var pack *domain.Pack
	if collectionID != "" {
		pack = &domain.Pack{
			Provider: domain.ProviderSteamWorkshop,
			Ref:      collectionID,
		}
		if h.cfg.Workshop != nil {
			col, err := h.cfg.Workshop.GetCollection(c.UserContext(), collectionID)
			if err != nil {
				return shared.SSEToast(c, "err", "Failed to resolve Steam Workshop collection: "+err.Error(), nil)
			}
			pack.Name = col.Title
		}
	}

	inst := domain.Instance{
		GameID: domain.GameGMod,
		Name:   name,
		Tier:   tier,
		Source: domain.SourceModpack,
		State:  domain.StateRunning,
		GMod: &domain.GModConfig{
			Pack:     pack,
			Gamemode: gamemode,
			Map:      mapName,
			Password: password,
		},
	}

	p := shared.PrincipalOf(c)
	inst.CreatedBy = p.Subject

	if !p.Admin && h.cfg.Requests != nil {
		direct, err := h.cfg.Requests.MayCreateDirectly(c.UserContext(), p.Subject)
		if err != nil {
			return shared.SSEToast(c, "err", "Could not check your world allowance: "+err.Error(), nil)
		}
		if !direct {
			submitted, err := h.cfg.Requests.Submit(c.UserContext(), p.Subject, inst, domain.ModList{})
			if err != nil {
				return shared.SSEToast(c, "err", err.Error(), nil)
			}
			return shared.SSEToast(c, "ok",
				fmt.Sprintf("Request for %q sent for approval. You already run a world, so this one needs a yes first.", submitted.Name),
				map[string]any{"redirect": "/gmod"})
		}
	}

	created, err := h.cfg.Instances.CreateInstance(c.UserContext(), inst, domain.ModList{}, h.cfg.Actor(c))
	if err != nil {
		return shared.SSEToast(c, "err", "Failed to create Garry's Mod server: "+err.Error(), nil)
	}

	if !p.Admin && h.cfg.Requests != nil {
		if err := h.cfg.Requests.GrantCreator(c.UserContext(), p.Subject, created.GameID, created.Number, h.cfg.Actor(c)); err != nil {
			slog.Error("gmod world created but creator not granted access", "number", created.Number, "err", err)
		}
	}

	_ = h.cfg.Instances.StartInstance(c.UserContext(), created.Number, h.cfg.Actor(c))

	return shared.SSEToast(c, "ok", fmt.Sprintf("Created Garry's Mod server %q! Redirecting...", created.Name), map[string]any{
		"redirect": fmt.Sprintf("/gmod/%d", created.Number),
	})
}

func (h *Handler) InstancePage(c *fiber.Ctx) error {
	if h.cfg.Instances == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Garry's Mod instance manager not configured")
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid instance number")
	}

	inst, err := h.cfg.Instances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return c.Status(fiber.StatusNotFound).SendString("Garry's Mod instance not found")
	}

	pass := ""
	gamemode := ""
	mapName := ""
	collectionID := ""
	if inst.GMod != nil {
		pass = inst.GMod.Password
		gamemode = inst.GMod.Gamemode
		mapName = inst.GMod.Map
		collectionID = inst.GMod.CollectionID()
	}

	lbIP := inst.LBIP
	if lbIP == "" && h.cfg.Instances != nil {
		if st, err := h.cfg.Instances.RuntimeStatus(c.UserContext(), inst.Number); err == nil && st.Address != "" {
			lbIP = st.Address
		}
	}

	d := pages.InstanceDetailUI{
		InstanceUI: pages.InstanceUI{
			GameID:       string(inst.GameID),
			Number:       inst.Number,
			Name:         inst.Name,
			Slug:         inst.Slug,
			Password:     pass,
			Gamemode:     gamemode,
			Map:          mapName,
			CollectionID: collectionID,
			Tier:         string(inst.Tier),
			MemoryGiB:    inst.MemoryGiB(domain.GModProfile),
			State:        string(inst.State),
			MOTD:         inst.MOTD,
			LBIP:         lbIP,
		},
		ActiveTab: "overview",
	}

	if collectionID != "" && h.cfg.Workshop != nil {
		if col, err := h.cfg.Workshop.GetCollection(c.UserContext(), collectionID); err == nil {
			d.CollectionTitle = col.Title
			d.CollectionItems = col.ItemCount
			if !col.TimeUpdated.IsZero() && !inst.CreatedAt.IsZero() && col.TimeUpdated.After(inst.CreatedAt) {
				d.RestartPending = true
				d.RestartReason = fmt.Sprintf("Steam Workshop collection %q was updated upstream on %s. Restart the server to apply changes.", col.Title, col.TimeUpdated.Format("Jan 02, 2006"))
			}
		}
	}

	return shared.Render(c, pages.GModInstanceDetail(d))
}

func (h *Handler) InstanceStart(c *fiber.Ctx) error {
	if h.cfg.Instances == nil {
		return shared.SSEToast(c, "err", "Garry's Mod instance manager not configured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	if err := h.cfg.Instances.StartInstance(c.UserContext(), num, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Start failed: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Starting Garry's Mod instance #%02d...", num), nil)
}

func (h *Handler) InstanceStop(c *fiber.Ctx) error {
	if h.cfg.Instances == nil {
		return shared.SSEToast(c, "err", "Garry's Mod instance manager not configured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	if err := h.cfg.Instances.StopInstance(c.UserContext(), num, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Stop failed: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Stopping Garry's Mod instance #%02d...", num), nil)
}

func (h *Handler) InstanceRestart(c *fiber.Ctx) error {
	if h.cfg.Instances == nil {
		return shared.SSEToast(c, "err", "Garry's Mod instance manager not configured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	if err := h.cfg.Instances.RestartInstance(c.UserContext(), num, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Restart failed: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Restarting Garry's Mod instance #%02d...", num), nil)
}

func (h *Handler) InstanceDelete(c *fiber.Ctx) error {
	if h.cfg.Instances == nil {
		return shared.SSEToast(c, "err", "Garry's Mod instance manager not configured.", nil)
	}

	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return shared.SSEToast(c, "err", "Invalid instance number.", nil)
	}

	if err := h.cfg.Instances.DeleteInstance(c.UserContext(), num, h.cfg.Actor(c)); err != nil {
		return shared.SSEToast(c, "err", "Failed to delete world: "+err.Error(), nil)
	}

	return shared.SSEToast(c, "ok", fmt.Sprintf("Deleted Garry's Mod instance #%02d.", num), map[string]any{
		"redirect": "/gmod",
	})
}
