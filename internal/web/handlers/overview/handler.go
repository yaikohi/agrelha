package overview

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"agrelha/internal/app/capacity"
	"agrelha/internal/app/games"
	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"

	"github.com/gofiber/fiber/v2"
)

type Config struct {
	Games            *games.Registry
	InstanceManagers map[domain.GameID]*instances.InstanceManager
	Capacity         *capacity.Service
	Actor            func(*fiber.Ctx) string
}

type Handler struct{ cfg Config }

func New(cfg Config) *Handler {
	if cfg.Actor == nil {
		cfg.Actor = shared.Actor
	}
	return &Handler{cfg: cfg}
}

func (h *Handler) Register(router fiber.Router) {
	router.Get("/overview", h.OverviewPage)
	router.Post("/overview/resources", h.SaveResources)
	router.Post("/overview/apply-tier", h.ApplyTier)
	router.Post("/overview/settings", h.SaveSettings)
	router.Post("/overview/tier", h.SaveTier)
	router.Post("/overview/allowance", h.SaveAllowance)
}

func (h *Handler) profiles() []domain.GameProfile {
	if h.cfg.Games != nil {
		var out []domain.GameProfile
		for _, e := range h.cfg.Games.Entries() {
			out = append(out, e.Profile)
		}
		if len(out) > 0 {
			return out
		}
	}
	return domain.Profiles()
}

func (h *Handler) manager(id domain.GameID) *instances.InstanceManager {
	if h.cfg.InstanceManagers == nil {
		return nil
	}
	return h.cfg.InstanceManagers[id]
}

func parseTarget(raw string) (domain.GameID, int, error) {
	parts := strings.SplitN(strings.TrimSpace(raw), ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("malformed target %q", raw)
	}
	game := domain.GameID(parts[0])
	if _, ok := domain.ProfileFor(game); !ok {
		return "", 0, fmt.Errorf("unknown game %q", parts[0])
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || n <= 0 {
		return "", 0, fmt.Errorf("bad world number %q", parts[1])
	}
	return game, n, nil
}

func formInt(c *fiber.Ctx, name string) int {
	n, err := strconv.Atoi(strings.TrimSpace(c.FormValue(name)))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// showHeap is derived from the game's own defaults rather than naming a game:
// only Minecraft tiers carry a JVM heap size, and a game that has never had one
// should not be offered the field.
func showHeap(id domain.GameID) bool {
	for _, t := range domain.DefaultTiersFor(id) {
		if t.HeapInitGiB > 0 {
			return true
		}
	}
	return false
}

func (h *Handler) OverviewPage(c *fiber.Ctx) error {
	fk, fm := shared.TakeFlash(c)
	ctx := c.UserContext()

	var worlds []pages.OverviewWorldUI
	var gamesUI []pages.OverviewGameUI

	for _, p := range h.profiles() {
		mgr := h.manager(p.ID)
		var list []domain.Instance
		if mgr != nil {
			got, err := mgr.ListInstances(ctx)
			if err != nil {
				return shared.Render(c, pages.Overview(nil, nil, 0, "err",
					fmt.Sprintf("Could not list %s worlds: %v", p.ID, err)))
			}
			list = got
		}

		tiers, err := h.cfg.Capacity.Tiers(ctx, p.ID)
		if err != nil {
			return shared.Render(c, pages.Overview(nil, nil, 0, "err",
				fmt.Sprintf("Could not read %s tiers: %v", p.ID, err)))
		}
		tierName := map[string]string{}
		inUse := map[string]int{}
		for _, t := range tiers {
			tierName[t.Key] = t.Name
		}

		for _, inst := range list {
			r := inst.EffectiveResources(p)
			inUse[string(inst.Tier)]++
			worlds = append(worlds, pages.OverviewWorldUI{
				Game: string(p.ID), Number: inst.Number, Name: inst.Name,
				State: string(inst.State), Tier: string(inst.Tier),
				MemReq: r.MemRequestGiB, MemLimit: r.MemLimitGiB,
				CPUReq: r.CPURequestMilli, CPULimit: r.CPULimitMilli,
				Drifts: inst.DriftsFromTier(p), TierLabel: tierName[string(inst.Tier)],
				CreatedBy: inst.CreatedBy,
			})
		}

		settings, err := h.cfg.Capacity.Settings(ctx, p.ID)
		if err != nil {
			return shared.Render(c, pages.Overview(nil, nil, 0, "err",
				fmt.Sprintf("Could not read %s settings: %v", p.ID, err)))
		}
		budget := domain.CalculateBudget(p, list, settings.TotalBudgetGiB, settings.MaxRunning, settings.MaxInstances)

		g := pages.OverviewGameUI{
			Game: string(p.ID), Display: p.Display.Name,
			TotalBudgetGiB: budget.TotalBudgetGiB, UsedGiB: budget.UsedGiB,
			MaxInstances: budget.MaxInstances, MaxRunning: budget.MaxRunning,
			CeilingMemGiB: settings.Ceiling.MemLimitGiB,
			ShowHeap:      showHeap(p.ID),
		}
		for _, t := range tiers {
			g.Tiers = append(g.Tiers, pages.OverviewTierUI{
				Key: t.Key, Name: t.Name,
				MemReq: t.Resources.MemRequestGiB, MemLimit: t.Resources.MemLimitGiB,
				CPUReq: t.Resources.CPURequestMilli, CPULimit: t.Resources.CPULimitMilli,
				HeapInit: t.HeapInitGiB, InUse: inUse[t.Key],
			})
		}
		gamesUI = append(gamesUI, g)
	}

	return shared.Render(c, pages.Overview(worlds, gamesUI, h.allowance(ctx), fk, fm))
}

func (h *Handler) allowance(ctx context.Context) int {
	if h.cfg.Capacity == nil {
		return 0
	}
	return h.cfg.Capacity.FreeCreations(ctx)
}

func (h *Handler) back(c *fiber.Ctx, kind, msg string) error {
	shared.SetFlash(c, kind, msg)
	return c.Redirect("/overview", fiber.StatusSeeOther)
}

func (h *Handler) SaveResources(c *fiber.Ctx) error {
	game, num, err := parseTarget(c.FormValue("target"))
	if err != nil {
		return h.back(c, "err", err.Error())
	}
	mgr := h.manager(game)
	if mgr == nil {
		return h.back(c, "err", fmt.Sprintf("no manager for %s", game))
	}
	r := domain.Resources{
		MemRequestGiB:   formInt(c, "memreq"),
		MemLimitGiB:     formInt(c, "memlimit"),
		CPURequestMilli: formInt(c, "cpureq"),
		CPULimitMilli:   formInt(c, "cpulimit"),
	}
	// Admins are not bound by the ceiling, but nonsense is still rejected.
	if err := h.cfg.Capacity.CheckResources(c.UserContext(), game, r, shared.IsAdmin(c)); err != nil {
		return h.back(c, "err", err.Error())
	}
	if err := mgr.SetResources(c.UserContext(), num, r, h.cfg.Actor(c)); err != nil {
		return h.back(c, "err", "Resize failed: "+err.Error())
	}
	return h.back(c, "ok", fmt.Sprintf("%s %02d resized to %d/%d GiB. It is restarting.",
		game, num, r.MemRequestGiB, r.MemLimitGiB))
}

func (h *Handler) ApplyTier(c *fiber.Ctx) error {
	game, num, err := parseTarget(c.FormValue("target"))
	if err != nil {
		return h.back(c, "err", err.Error())
	}
	mgr := h.manager(game)
	if mgr == nil {
		return h.back(c, "err", fmt.Sprintf("no manager for %s", game))
	}
	inst, err := mgr.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return h.back(c, "err", fmt.Sprintf("world %s %02d not found", game, num))
	}
	tier, err := h.cfg.Capacity.Tier(c.UserContext(), game, string(inst.Tier))
	if err != nil || tier == nil {
		return h.back(c, "err", fmt.Sprintf("no tier %q defined for %s", inst.Tier, game))
	}
	if err := mgr.ApplyTier(c.UserContext(), num, *tier, h.cfg.Actor(c)); err != nil {
		return h.back(c, "err", "Could not apply tier: "+err.Error())
	}
	return h.back(c, "ok", fmt.Sprintf("%s %02d restored to %s. It is restarting.", game, num, tier.Name))
}

func (h *Handler) SaveSettings(c *fiber.Ctx) error {
	game := domain.GameID(strings.TrimSpace(c.FormValue("game")))
	if _, ok := domain.ProfileFor(game); !ok {
		return h.back(c, "err", fmt.Sprintf("unknown game %q", game))
	}
	s := domain.GameSettings{
		GameID:         game,
		TotalBudgetGiB: formInt(c, "budget"),
		MaxInstances:   formInt(c, "maxinstances"),
		MaxRunning:     formInt(c, "maxrunning"),
		Ceiling:        domain.Resources{MemLimitGiB: formInt(c, "ceiling")},
	}
	if err := h.cfg.Capacity.PutSettings(c.UserContext(), s, h.cfg.Actor(c)); err != nil {
		return h.back(c, "err", "Could not save limits: "+err.Error())
	}
	return h.back(c, "ok", fmt.Sprintf("Limits for %s saved.", game))
}

func (h *Handler) SaveTier(c *fiber.Ctx) error {
	game := domain.GameID(strings.TrimSpace(c.FormValue("game")))
	if _, ok := domain.ProfileFor(game); !ok {
		return h.back(c, "err", fmt.Sprintf("unknown game %q", game))
	}
	key := strings.TrimSpace(c.FormValue("key"))
	existing, _ := h.cfg.Capacity.Tier(c.UserContext(), game, key)
	t := domain.Tier{Key: key, GameID: game, Name: key}
	if existing != nil {
		t = *existing
	}
	t.Resources = domain.Resources{
		MemRequestGiB:   formInt(c, "memreq"),
		MemLimitGiB:     formInt(c, "memlimit"),
		CPURequestMilli: formInt(c, "cpureq"),
		CPULimitMilli:   formInt(c, "cpulimit"),
	}
	if showHeap(game) {
		t.HeapInitGiB = formInt(c, "heap")
	}
	if err := h.cfg.Capacity.PutTier(c.UserContext(), t, h.cfg.Actor(c)); err != nil {
		return h.back(c, "err", "Could not save tier: "+err.Error())
	}
	return h.back(c, "ok", fmt.Sprintf("Tier %s/%s saved. Existing worlds are unchanged.", game, key))
}

func (h *Handler) SaveAllowance(c *fiber.Ctx) error {
	n := formInt(c, "allowance")
	if err := h.cfg.Capacity.SetFreeCreations(c.UserContext(), n, h.cfg.Actor(c)); err != nil {
		return h.back(c, "err", "Could not save allowance: "+err.Error())
	}
	return h.back(c, "ok", fmt.Sprintf("Accounts may now create %d world(s) before asking.", n))
}
