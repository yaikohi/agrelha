package access

import (
	"fmt"
	"strings"

	"agrelha/internal/domain"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"

	"github.com/gofiber/fiber/v2"
)

func (h *Handler) AdminsPage(c *fiber.Ctx) error {
	game := c.Query("game", "valheim")
	return h.renderAdminsPage(c, game)
}

func (h *Handler) renderAdminsPage(c *fiber.Ctx, activeGame string) error {
	if activeGame != "minecraft" && activeGame != "gmod" && activeGame != "valheim" {
		activeGame = "valheim"
	}

	var players []domain.Player
	if h.cfg.Players != nil {
		players, _ = h.cfg.Players.ListPlayers()
	}
	var adminIDs []string
	if h.cfg.Admins != nil {
		adminIDs, _ = h.cfg.Admins.List(c.UserContext())
	}
	var valheimPasswords []pages.ValheimWorldPasswordUI
	if h.cfg.ValheimInstances != nil {
		if insts, err := h.cfg.ValheimInstances.ListInstances(c.UserContext()); err == nil {
			for _, inst := range insts {
				addr := ""
				if inst.LBIP != "" {
					addr = fmt.Sprintf("%s:2456", inst.LBIP)
				}
				pass := ""
				if inst.Valheim != nil {
					pass = inst.Valheim.Password
				}
				valheimPasswords = append(valheimPasswords, pages.ValheimWorldPasswordUI{
					Number:   inst.Number,
					Name:     inst.Name,
					State:    string(inst.State),
					Password: pass,
					Address:  addr,
				})
			}
		}
	}

	var ops []string
	var whitelist []string
	var onlinePlayers []string
	whitelistEnforced := false
	if h.cfg.MCAccess != nil {
		if o, w, err := h.cfg.MCAccess.ListAccess(c.UserContext()); err == nil {
			ops = o
			whitelist = w
		}
		if pl, err := h.cfg.MCAccess.OnlinePlayers(); err == nil {
			onlinePlayers = pl
		}
		if enf, err := h.cfg.MCAccess.WhitelistEnforced(); err == nil {
			whitelistEnforced = enf
		}
	}

	var gmodPasswords []pages.GModWorldPasswordUI
	if h.cfg.GModInstances != nil {
		if insts, err := h.cfg.GModInstances.ListInstances(c.UserContext()); err == nil {
			for _, inst := range insts {
				addr := pages.ConnectAddress(inst.LBIP, 27015)
				pass := ""
				if inst.GMod != nil {
					pass = inst.GMod.Password
				}
				gmodPasswords = append(gmodPasswords, pages.GModWorldPasswordUI{
					Number:   inst.Number,
					Name:     inst.Name,
					State:    string(inst.State),
					Password: pass,
					Address:  addr,
					LBIP:     inst.LBIP,
				})
			}
		}
	}

	fk, fm := shared.TakeFlash(c)
	return shared.Render(c, pages.Admins(pages.AdminsProps{
		ActiveGame:        activeGame,
		FlashKind:         fk,
		FlashMsg:          fm,
		ValheimPlayers:    players,
		ValheimAdminIDs:   adminIDs,
		ValheimPasswords:  valheimPasswords,
		ValheimEnabled:    h.cfg.Admins != nil,
		MCOps:             ops,
		MCWhitelist:       whitelist,
		MCOnlinePlayers:   onlinePlayers,
		MCWhitelistActive: whitelistEnforced,
		MCEnabled:         h.cfg.StateStore != nil,
		GModPasswords:     gmodPasswords,
	}))
}

func (h *Handler) AdminsGrant(c *fiber.Ctx) error {
	if h.cfg.Admins == nil {
		shared.SetFlash(c, "err", "Declarative plane disabled — no Git token configured.")
		return c.Redirect("/admins?game=valheim", fiber.StatusSeeOther)
	}
	id := c.FormValue("steam_id")
	if id == "" {
		shared.SetFlash(c, "err", "A Steam64 ID is required.")
		return c.Redirect("/admins?game=valheim", fiber.StatusSeeOther)
	}
	changed, err := h.cfg.Admins.Grant(c.UserContext(), id, h.cfg.Actor(c))
	if err != nil {
		shared.SetFlash(c, "err", "Grant commit failed: "+err.Error())
		return c.Redirect("/admins?game=valheim", fiber.StatusSeeOther)
	}
	if changed {
		if h.cfg.ApplyAfterSync != nil {
			h.cfg.ApplyAfterSync("valheim-admins", "ADMINLIST_IDS", func(v string) bool {
				return strings.Contains(" "+v+" ", " "+id+" ")
			})
		}
		shared.SetFlash(c, "ok", "Granted admin to "+id+" — committed; applies on the next server restart.")
	} else {
		shared.SetFlash(c, "ok", id+" is already an admin.")
	}
	return c.Redirect("/admins?game=valheim", fiber.StatusSeeOther)
}

func (h *Handler) AdminsRevoke(c *fiber.Ctx) error {
	if h.cfg.Admins == nil {
		shared.SetFlash(c, "err", "Declarative plane disabled — no Git token configured.")
		return c.Redirect("/admins?game=valheim", fiber.StatusSeeOther)
	}
	id := c.FormValue("steam_id")
	if id == "" {
		shared.SetFlash(c, "err", "A Steam64 ID is required.")
		return c.Redirect("/admins?game=valheim", fiber.StatusSeeOther)
	}
	changed, err := h.cfg.Admins.Revoke(c.UserContext(), id, h.cfg.Actor(c))
	if err != nil {
		shared.SetFlash(c, "err", "Revoke commit failed: "+err.Error())
		return c.Redirect("/admins?game=valheim", fiber.StatusSeeOther)
	}
	if changed {
		if h.cfg.ApplyAfterSync != nil {
			h.cfg.ApplyAfterSync("valheim-admins", "ADMINLIST_IDS", func(v string) bool {
				return !strings.Contains(" "+v+" ", " "+id+" ")
			})
		}
		shared.SetFlash(c, "ok", "Revoked admin from "+id+" — committed; applies on the next server restart.")
	} else {
		shared.SetFlash(c, "ok", id+" was not an admin.")
	}
	return c.Redirect("/admins?game=valheim", fiber.StatusSeeOther)
}
