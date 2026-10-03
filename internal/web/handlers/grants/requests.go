package grants

import (
	"strings"

	"agrelha/internal/domain"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"

	"github.com/gofiber/fiber/v2"
)

func (h *Handler) label(subject string, accounts map[string]string) string {
	if l, ok := accounts[subject]; ok && l != "" {
		return l
	}
	return subject
}

func toRequestUI(r domain.InstanceRequest, label string) pages.RequestUI {
	mods := 0
	if s := strings.TrimSpace(r.Mods.Primary); s != "" {
		mods = len(strings.Fields(s))
	}
	ui := pages.RequestUI{
		ID:        r.ID,
		Requester: label,
		Game:      string(r.GameID),
		Name:      r.Name,
		Tier:      string(r.Instance.Tier),
		Mods:      mods,
		Created:   r.CreatedAt.Format(timeFmt),
		Status:    string(r.Status),
		Note:      r.Note,
		DecidedBy: r.DecidedBy,
		Number:    r.Number,
	}
	if !r.DecidedAt.IsZero() {
		ui.Decided = r.DecidedAt.Format(timeFmt)
	}
	return ui
}

func (h *Handler) accountLabels(c *fiber.Ctx) map[string]string {
	out := map[string]string{}
	accounts, err := h.cfg.Authz.ListAccounts(c.UserContext())
	if err != nil {
		return out
	}
	for _, a := range accounts {
		out[a.Subject] = a.Label()
	}
	return out
}

func (h *Handler) RequestsPage(c *fiber.Ctx) error {
	fk, fm := shared.TakeFlash(c)
	if h.cfg.Requests == nil {
		return shared.Render(c, pages.Requests(nil, nil, "err", "World requests are not configured."))
	}
	all, err := h.cfg.Requests.All(c.UserContext())
	if err != nil {
		return shared.Render(c, pages.Requests(nil, nil, "err", "Could not load requests: "+err.Error()))
	}
	labels := h.accountLabels(c)

	var pending, decided []pages.RequestUI
	for _, r := range all {
		ui := toRequestUI(r, h.label(r.Subject, labels))
		if r.Pending() {
			pending = append(pending, ui)
			continue
		}
		decided = append(decided, ui)
	}
	return shared.Render(c, pages.Requests(pending, decided, fk, fm))
}

func (h *Handler) ApproveRequest(c *fiber.Ctx) error {
	id := strings.TrimSpace(c.FormValue("id"))
	created, err := h.cfg.Requests.Approve(c.UserContext(), id, h.cfg.Actor(c))
	if err != nil {
		shared.SetFlash(c, "err", "Could not approve: "+err.Error())
		return c.Redirect("/requests", fiber.StatusSeeOther)
	}
	shared.SetFlash(c, "ok", "Created "+created.Name+" and granted the requester access to it.")
	return c.Redirect("/requests", fiber.StatusSeeOther)
}

func (h *Handler) DenyRequest(c *fiber.Ctx) error {
	id := strings.TrimSpace(c.FormValue("id"))
	note := strings.TrimSpace(c.FormValue("note"))
	if err := h.cfg.Requests.Deny(c.UserContext(), id, note, h.cfg.Actor(c)); err != nil {
		shared.SetFlash(c, "err", "Could not deny: "+err.Error())
		return c.Redirect("/requests", fiber.StatusSeeOther)
	}
	shared.SetFlash(c, "ok", "Request denied.")
	return c.Redirect("/requests", fiber.StatusSeeOther)
}
