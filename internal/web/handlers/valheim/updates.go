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
	rep := h.cfg.ModUpdates.Report(inst.Number)
	d.ModUpdates = pages.ModUpdateViews(rep.Updates)
	d.ModsMissing = pages.ModRefNames(rep.Missing)
	d.ModsUnreachable = pages.ModRefNames(rep.Unreachable)
	d.ModsChecked, d.ModsTotal = rep.Checked, rep.Total
	if !rep.At.IsZero() {
		d.UpdatesChecked = domain.FormatDuration(time.Since(rep.At))
	}
	d.UpdatesPending = h.cfg.ModUpdates.Pending(ctx, inst.Number)
	if err := h.cfg.ModUpdates.LastError(inst.Number); err != nil {
		d.UpdatesError = "Last check failed: " + err.Error()
	}

	if rp, err := h.cfg.ModUpdates.RestoreAvailable(ctx, inst.Number); err == nil && rp != nil {
		d.CanUndo = true
		d.UndoWhen = domain.FormatDuration(time.Since(rp.At)) + " ago"
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

	rep, err := h.cfg.ModUpdates.RefreshOne(c.UserContext(), inst.Number)
	if err != nil {
		return shared.SSEToast(c, "err", "Check failed: "+err.Error(), nil)
	}

	msg, kind := "No mod updates available.", "ok"
	if len(rep.Updates) > 0 {
		msg = fmt.Sprintf("%d mod update(s) available.", len(rep.Updates))
	}
	if n := len(rep.Unreachable); n > 0 {
		msg += fmt.Sprintf(" %d mod(s) could not be checked.", n)
	}
	if n := len(rep.Missing); n > 0 {
		msg = fmt.Sprintf("%d installed mod(s) are gone from Thunderstore — this world will fail to boot.", n)
		kind = "err"
	}
	return h.pushUpdatePanel(c, *inst, msg, kind)
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

// ValheimModUpdatesUndo restores the mod list from before the last update.
func (h *Handler) ValheimModUpdatesUndo(c *fiber.Ctx) error {
	inst, err := h.updatableInstance(c)
	if err != nil {
		return shared.SSEToast(c, "err", err.Error(), nil)
	}
	if h.cfg.ModUpdates == nil {
		return shared.SSEToast(c, "err", "Mod update checking is not configured.", nil)
	}

	rp, err := h.cfg.ModUpdates.Undo(c.UserContext(), inst.Number, h.cfg.Actor(c))
	if err != nil {
		return shared.SSEToast(c, "err", "Undo failed: "+err.Error(), nil)
	}
	return h.pushUpdatePanel(c, *inst,
		fmt.Sprintf("Reverted to the %d mod(s) from before the update — this world restarts once ArgoCD syncs.", len(rp.Previous)),
		"ok")
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
