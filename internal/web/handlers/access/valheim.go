package access

import (
	"fmt"
	"strings"

	"agrelha/internal/domain"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"

	"github.com/gofiber/fiber/v2"
)

func (h *Handler) AccessPage(c *fiber.Ctx) error {
	game := c.Query("game", "valheim")
	return h.renderAccessPage(c, game)
}

func (h *Handler) AdminsPage(c *fiber.Ctx) error {
	game := c.Query("game", "")
	target := "/access"
	if game != "" {
		target += "?game=" + game
	}
	return c.Redirect(target, fiber.StatusMovedPermanently)
}

func (h *Handler) renderAccessPage(c *fiber.Ctx, activeGame string) error {
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
					gamePort, _, _ := domain.ValheimHostPorts(inst)
					addr = fmt.Sprintf("%s:%d", inst.LBIP, gamePort)
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
				port, err := domain.HostPortFor(domain.GModProfile, inst.EffectiveHost(), inst.Number, domain.GModGamePort)
				if err != nil {
					port = domain.GModGamePort
				}
				addr := pages.ConnectAddress(inst.LBIP, port)
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
					Port:     port,
				})
			}
		}
	}

	fk, fm := shared.TakeFlash(c)
	return shared.Render(c, pages.Access(pages.AccessProps{
		ActiveGame: activeGame,
		FlashKind:  fk,
		FlashMsg:   fm,
		Valheim: pages.ValheimAccessProps{
			Players:   players,
			AdminIDs:  adminIDs,
			Passwords: valheimPasswords,
			Enabled:   h.cfg.Admins != nil,
		},
		Minecraft: pages.MCAccessProps{
			Ops:             ops,
			Whitelist:       whitelist,
			OnlinePlayers:   onlinePlayers,
			WhitelistActive: whitelistEnforced,
			Enabled:         h.cfg.StateStore != nil,
		},
		GMod: pages.GModAccessProps{
			Passwords: gmodPasswords,
		},
	}))
}

func (h *Handler) AccessGrant(c *fiber.Ctx) error {
	if h.cfg.Admins == nil {
		shared.SetFlash(c, "err", "Declarative plane disabled — no Git token configured.")
		return c.Redirect("/access?game=valheim", fiber.StatusSeeOther)
	}
	id := c.FormValue("steam_id")
	if id == "" {
		shared.SetFlash(c, "err", "A Steam64 ID is required.")
		return c.Redirect("/access?game=valheim", fiber.StatusSeeOther)
	}
	changed, err := h.cfg.Admins.Grant(c.UserContext(), id, h.cfg.Actor(c))
	if err != nil {
		shared.SetFlash(c, "err", "Grant commit failed: "+err.Error())
		return c.Redirect("/access?game=valheim", fiber.StatusSeeOther)
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
	return c.Redirect("/access?game=valheim", fiber.StatusSeeOther)
}

func (h *Handler) AdminsGrant(c *fiber.Ctx) error {
	return h.AccessGrant(c)
}

func (h *Handler) AccessRevoke(c *fiber.Ctx) error {
	if h.cfg.Admins == nil {
		shared.SetFlash(c, "err", "Declarative plane disabled — no Git token configured.")
		return c.Redirect("/access?game=valheim", fiber.StatusSeeOther)
	}
	id := c.FormValue("steam_id")
	if id == "" {
		shared.SetFlash(c, "err", "A Steam64 ID is required.")
		return c.Redirect("/access?game=valheim", fiber.StatusSeeOther)
	}
	changed, err := h.cfg.Admins.Revoke(c.UserContext(), id, h.cfg.Actor(c))
	if err != nil {
		shared.SetFlash(c, "err", "Revoke commit failed: "+err.Error())
		return c.Redirect("/access?game=valheim", fiber.StatusSeeOther)
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
	return c.Redirect("/access?game=valheim", fiber.StatusSeeOther)
}

func (h *Handler) AdminsRevoke(c *fiber.Ctx) error {
	return h.AccessRevoke(c)
}
