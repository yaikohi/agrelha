package valheim

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/domain"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
	"agrelha/internal/web/sse"
)

// ModUpdateState fills in the mod-update half of an instance view: what the
// last check found, whether an applied update is still landing, and the player
// count the confirmation prompt needs.
func (h *Handler) ModUpdateState(ctx context.Context, d *pages.InstanceDetailUI, inst domain.Instance) {
	if h.cfg.ModUpdates == nil {
		return
	}
	d.ModUpdates = pages.ModUpdateViews(h.cfg.ModUpdates.Updates(inst.Number))
	d.UpdatesPending = h.cfg.ModUpdates.Pending(ctx, inst.Number)
	if at := h.cfg.ModUpdates.CheckedAt(inst.Number); !at.IsZero() {
		d.UpdatesChecked = domain.FormatDuration(time.Since(at))
	}
	if inst.State == domain.StateRunning && h.cfg.ValheimInstances != nil {
		if st, ok := h.cfg.ValheimInstances.InstanceStats(ctx, []domain.Instance{inst})[inst.Number]; ok {
			d.Players, d.PlayersKnown = st.Players, st.PlayersKnown
		}
	}
}

// ValheimModUpdatesCheck re-checks one world against Thunderstore on demand,
// rather than waiting for the background refresh.
func (h *Handler) ValheimModUpdatesCheck(c *fiber.Ctx) error {
	inst, err := h.updatableInstance(c)
	if err != nil {
		return shared.SSEToast(c, "err", err.Error(), nil)
	}
	if h.cfg.ModUpdates == nil {
		return shared.SSEToast(c, "err", "Mod update checking is not configured.", nil)
	}

	ups, err := h.cfg.ModUpdates.RefreshOne(c.UserContext(), inst.Number)
	if err != nil {
		return shared.SSEToast(c, "err", "Check failed: "+err.Error(), nil)
	}

	msg := "No mod updates available."
	if len(ups) > 0 {
		msg = fmt.Sprintf("%d mod update(s) available.", len(ups))
	}
	return h.pushUpdatePanel(c, *inst, msg, "ok")
}

// ValheimModUpdatesApply repins the selected mods to their latest versions.
func (h *Handler) ValheimModUpdatesApply(c *fiber.Ctx) error {
	inst, err := h.updatableInstance(c)
	if err != nil {
		return shared.SSEToast(c, "err", err.Error(), nil)
	}
	if h.cfg.ModUpdates == nil {
		return shared.SSEToast(c, "err", "Mod update checking is not configured.", nil)
	}

	var body struct {
		All      bool            `json:"all"`
		Selected map[string]bool `json:"selected"`
	}
	_ = json.Unmarshal(c.Body(), &body)

	ctx := c.UserContext()
	var keys []string
	if !body.All {
		for _, u := range h.cfg.ModUpdates.Updates(inst.Number) {
			if body.Selected[pages.UpdateToken(u.Ref.Key())] {
				keys = append(keys, u.Ref.Key())
			}
		}
		if len(keys) == 0 {
			return shared.SSEToast(c, "err", "No mods selected to update.", nil)
		}
	}

	if h.cfg.ModUpdates.Pending(ctx, inst.Number) {
		return shared.SSEToast(c, "ok", "An update is already in progress — this world restarts once ArgoCD syncs.", nil)
	}

	applied, err := h.cfg.ModUpdates.Apply(ctx, inst.Number, keys, h.cfg.Actor(c))
	if err != nil {
		return shared.SSEToast(c, "err", "Update failed: "+err.Error(), nil)
	}
	if len(applied) == 0 {
		return h.pushUpdatePanel(c, *inst, "Already up to date.", "ok")
	}

	msg := fmt.Sprintf("Updating %d mod(s) — this world restarts once ArgoCD syncs.", len(applied))
	return h.pushUpdatePanel(c, *inst, msg, "ok")
}

func (h *Handler) updatableInstance(c *fiber.Ctx) (*domain.Instance, error) {
	if h.cfg.ValheimInstances == nil {
		return nil, fmt.Errorf("Valheim instance manager unconfigured")
	}
	num, err := strconv.Atoi(c.Params("num"))
	if err != nil {
		return nil, fmt.Errorf("invalid instance number")
	}
	inst, err := h.cfg.ValheimInstances.GetInstance(c.UserContext(), num)
	if err != nil || inst == nil {
		return nil, fmt.Errorf("Valheim instance not found")
	}
	if inst.IsVanilla() {
		return nil, inst.VanillaImmutableErr()
	}
	return inst, nil
}

func (h *Handler) pushUpdatePanel(c *fiber.Ctx, inst domain.Instance, msg, kind string) error {
	d := pages.InstanceDetailUI{
		InstanceUI: pages.InstanceUI{
			Number: inst.Number,
			Name:   inst.Name,
			State:  string(inst.State),
		},
	}
	h.ModUpdateState(c.UserContext(), &d, inst)

	var panel bytes.Buffer
	if err := pages.ModUpdatePanel(d).Render(c.UserContext(), &panel); err != nil {
		return shared.SSEToast(c, "err", "Render failed: "+err.Error(), nil)
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := sse.InnerElement(w, "#mod-updates-panel", panel.String()); err != nil {
		return err
	}
	if err := sse.PatchSignals(w, map[string]any{
		"toast":     msg,
		"toastkind": kind,
		"selected":  map[string]any{},
	}); err != nil {
		return err
	}
	_ = w.Flush()
	return c.Send(buf.Bytes())
}
