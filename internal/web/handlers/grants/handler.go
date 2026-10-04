package grants

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"agrelha/internal/app/authz"
	"agrelha/internal/app/instances"
	"agrelha/internal/app/requests"
	"agrelha/internal/domain"
	"agrelha/internal/ports"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"

	"github.com/gofiber/fiber/v2"
)

type Config struct {
	Authz            *authz.Service
	Requests         *requests.Service
	ValheimInstances *instances.InstanceManager
	MCInstances      *instances.InstanceManager
	GModInstances    *instances.InstanceManager
	Actor            func(*fiber.Ctx) string
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

func (h *Handler) Register(router fiber.Router) {
	router.Get("/accounts", h.AccountsPage)
	router.Post("/accounts/grant", h.Grant)
	router.Post("/accounts/revoke", h.Revoke)
	router.Post("/accounts/sessions/revoke", h.RevokeSessions)
	router.Get("/requests", h.RequestsPage)
	router.Post("/requests/approve", h.ApproveRequest)
	router.Post("/requests/deny", h.DenyRequest)
}

const timeFmt = "2006-01-02 15:04"

func parseTarget(raw string) (domain.GameID, int, error) {
	parts := strings.SplitN(strings.TrimSpace(raw), ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("malformed target %q", raw)
	}
	game := domain.GameID(parts[0])
	if _, ok := domain.ProfileFor(game); !ok {
		return "", 0, fmt.Errorf("unknown game %q", parts[0])
	}
	num, err := strconv.Atoi(parts[1])
	if err != nil || num <= 0 {
		return "", 0, fmt.Errorf("bad instance number %q", parts[1])
	}
	return game, num, nil
}

func (h *Handler) instanceOptions(c *fiber.Ctx) ([]pages.InstanceOptionUI, map[string]string) {
	var out []pages.InstanceOptionUI
	labels := map[string]string{}
	for _, m := range []*instances.InstanceManager{h.cfg.ValheimInstances, h.cfg.MCInstances, h.cfg.GModInstances} {
		if m == nil {
			continue
		}
		list, err := m.ListInstances(c.UserContext())
		if err != nil {
			slog.Warn("accounts: cannot list instances", "err", err)
			continue
		}
		for _, inst := range list {
			target := fmt.Sprintf("%s:%d", inst.GameID, inst.Number)
			label := fmt.Sprintf("%s %02d - %s", inst.GameID, inst.Number, inst.Name)
			labels[string(domain.InstanceRole(inst.GameID, inst.Number))] = label
			out = append(out, pages.InstanceOptionUI{Target: target, Label: label})
		}
	}
	return out, labels
}

func (h *Handler) AccountsPage(c *fiber.Ctx) error {
	fk, fm := shared.TakeFlash(c)
	options, labels := h.instanceOptions(c)

	registryReady := h.cfg.Authz != nil && h.cfg.Authz.HasRegistry()
	note := "ZITADEL_PROJECT_ID and ZITADEL_SERVICE_KEY are not configured, so agrelha cannot read or change grants in Zitadel."

	accounts, err := h.cfg.Authz.ListAccounts(c.UserContext())
	if err != nil {
		return shared.Render(c, pages.Accounts(nil, options, registryReady, note, "err", "Could not load accounts: "+err.Error()))
	}

	ui := make([]pages.AccountUI, 0, len(accounts))
	for _, a := range accounts {
		row := pages.AccountUI{
			Subject:   a.Subject,
			Email:     a.Email,
			Name:      a.Name,
			Label:     a.Label(),
			FirstSeen: a.FirstSeen.Format(timeFmt),
			LastSeen:  a.LastSeen.Format(timeFmt),
		}
		if registryReady {
			roles, err := h.cfg.Authz.RolesFor(c.UserContext(), a.Subject)
			if err != nil {
				slog.Warn("accounts: cannot read roles", "subject", a.Subject, "err", err)
				registryReady = false
				note = "Zitadel did not answer a role lookup: " + err.Error()
			}
			for _, r := range roles {
				if r == domain.RoleAdmin {
					row.Admin = true
					continue
				}
				game, num, ok := domain.ParseInstanceRole(r)
				if !ok {
					continue
				}
				label := labels[string(r)]
				if label == "" {
					label = fmt.Sprintf("%s %02d (deleted)", game, num)
				}
				row.Grants = append(row.Grants, pages.AccountGrantUI{
					Target: fmt.Sprintf("%s:%d", game, num),
					Label:  label,
				})
			}
		}
		ui = append(ui, row)
	}

	return shared.Render(c, pages.Accounts(ui, options, registryReady, note, fk, fm))
}

func (h *Handler) Grant(c *fiber.Ctx) error {
	subject := strings.TrimSpace(c.FormValue("subject"))
	game, num, err := parseTarget(c.FormValue("target"))
	if err != nil {
		shared.SetFlash(c, "err", err.Error())
		return c.Redirect("/accounts", fiber.StatusSeeOther)
	}
	if err := h.cfg.Authz.Grant(c.UserContext(), subject, game, num, h.cfg.Actor(c)); err != nil {
		if errors.Is(err, ports.ErrNoAccount) {
			shared.SetFlash(c, "err", "That account has never signed in, so it cannot be granted access yet.")
		} else {
			shared.SetFlash(c, "err", "Grant failed: "+err.Error())
		}
		return c.Redirect("/accounts", fiber.StatusSeeOther)
	}
	shared.SetFlash(c, "ok", fmt.Sprintf("Granted %s %02d. It applies when they next sign in.", game, num))
	return c.Redirect("/accounts", fiber.StatusSeeOther)
}

func (h *Handler) Revoke(c *fiber.Ctx) error {
	subject := strings.TrimSpace(c.FormValue("subject"))
	game, num, err := parseTarget(c.FormValue("target"))
	if err != nil {
		shared.SetFlash(c, "err", err.Error())
		return c.Redirect("/accounts", fiber.StatusSeeOther)
	}
	if err := h.cfg.Authz.Revoke(c.UserContext(), subject, game, num, h.cfg.Actor(c)); err != nil {
		shared.SetFlash(c, "err", "Revoke failed: "+err.Error())
		return c.Redirect("/accounts", fiber.StatusSeeOther)
	}
	shared.SetFlash(c, "ok", fmt.Sprintf("Revoked %s %02d. Use 'Sign out everywhere' to end their current session now.", game, num))
	return c.Redirect("/accounts", fiber.StatusSeeOther)
}

func (h *Handler) RevokeSessions(c *fiber.Ctx) error {
	subject := strings.TrimSpace(c.FormValue("subject"))
	if err := h.cfg.Authz.RevokeSessions(c.UserContext(), subject, h.cfg.Actor(c)); err != nil {
		shared.SetFlash(c, "err", "Could not end sessions: "+err.Error())
		return c.Redirect("/accounts", fiber.StatusSeeOther)
	}
	shared.SetFlash(c, "ok", "Signed that account out of every browser.")
	return c.Redirect("/accounts", fiber.StatusSeeOther)
}
