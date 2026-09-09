package content

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/mods"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
)

// EntryVersion extracts the version segment from an entry formatted as namespace/name/version.
func EntryVersion(entry string) string {
	p := strings.Split(strings.TrimSpace(entry), "/")
	if len(p) < 3 {
		return ""
	}
	return p[len(p)-1]
}

// VersionNewer reports whether version string a is strictly newer than b.
func VersionNewer(a, b string) bool {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(as) {
			av, _ = strconv.Atoi(strings.TrimSpace(as[i]))
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(strings.TrimSpace(bs[i]))
		}
		if av != bv {
			return av > bv
		}
	}
	return false
}

const PendingTTL = 6 * time.Minute

// SetPending records that a resolved set was just committed and is waiting for
// ArgoCD to reconcile + the pod to roll.
func (h *Handler) SetPending(entries []string) {
	if h.cfg.SetPending != nil {
		h.cfg.SetPending(entries)
		return
	}
	set := make(map[string]bool, len(entries))
	for _, e := range entries {
		set[e] = true
	}
	h.pendMu.Lock()
	h.pendSet = set
	h.pendAt = time.Now()
	h.pendMu.Unlock()
}

// PendingActive checks whether committed updates are still pending cluster rollout.
func (h *Handler) PendingActive(ctx context.Context) bool {
	if h.cfg.PendingActive != nil {
		return h.cfg.PendingActive(ctx)
	}
	h.pendMu.Lock()
	set, at := h.pendSet, h.pendAt
	h.pendMu.Unlock()
	if set == nil {
		return false
	}
	if time.Since(at) > PendingTTL {
		h.ClearPending()
		return false
	}
	if h.cfg.K8s == nil {
		return true
	}
	data, err := h.cfg.K8s.ConfigMapData(ctx, "valheim-mods")
	if err != nil {
		return true
	}
	have := map[string]bool{}
	for _, e := range mods.Parse(data["mods.txt"]) {
		have[e] = true
	}
	for e := range set {
		if !have[e] {
			return true
		}
	}
	h.ClearPending()
	return false
}

// ClearPending clears any pending update state.
func (h *Handler) ClearPending() {
	h.pendMu.Lock()
	h.pendSet = nil
	h.pendMu.Unlock()
}

// ModUpdates computes the list of Valheim mods with newer versions available on Thunderstore.
func (h *Handler) ModUpdates(ctx context.Context) []pages.ModUpdate {
	if h.cfg.K8s == nil || h.cfg.TS == nil {
		return nil
	}
	data, err := h.cfg.K8s.ConfigMapData(ctx, "valheim-mods")
	if err != nil {
		return nil
	}
	var out []pages.ModUpdate
	for _, e := range mods.Parse(data["mods.txt"]) {
		key := pages.ModKey(e)
		cur := EntryVersion(e)
		m, ok := h.cfg.TS.Get(key)
		if !ok || m.Version == "" || cur == "" {
			continue
		}
		if VersionNewer(m.Version, cur) {
			out = append(out, pages.ModUpdate{Key: key, Current: cur, Latest: m.Version, Token: pages.UpdateToken(key)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// ModsUpdateAll triggers updates for all installed mods that have updates.
func (h *Handler) ModsUpdateAll(c *fiber.Ctx) error {
	var keys []string
	for _, u := range h.ModUpdates(c.UserContext()) {
		keys = append(keys, u.Key)
	}
	return h.ApplyUpdates(c, keys)
}

// ModsUpdateSelected triggers updates for a subset of mods selected by the user.
func (h *Handler) ModsUpdateSelected(c *fiber.Ctx) error {
	var body struct {
		Selected map[string]bool `json:"selected"`
	}
	_ = json.Unmarshal(c.Body(), &body)

	var keys []string
	for _, u := range h.ModUpdates(c.UserContext()) {
		if body.Selected[u.Token] {
			keys = append(keys, u.Key)
		}
	}
	if len(keys) == 0 {
		return shared.SSEToast(c, "err", "No mods selected to update.", nil)
	}
	return h.ApplyUpdates(c, keys)
}

// ApplyUpdates resolves, commits, and watches rollout for the specified mod keys.
func (h *Handler) ApplyUpdates(c *fiber.Ctx, targetKeys []string) error {
	if h.cfg.Mods == nil {
		return shared.SSEToast(c, "err", "Declarative plane disabled — no Codeberg token configured.", nil)
	}
	if len(targetKeys) == 0 {
		return shared.SSEToast(c, "ok", "All mods are already up to date.", nil)
	}
	if h.cfg.TS == nil {
		return shared.SSEToast(c, "err", "Thunderstore client not available.", nil)
	}
	ctx := c.UserContext()

	if h.PendingActive(ctx) {
		return shared.SSEToast(c, "ok", "An update is already in progress — the server will restart once ArgoCD syncs.", nil)
	}

	if h.cfg.K8s == nil {
		return shared.SSEToast(c, "err", "Cluster client not available.", nil)
	}
	data, err := h.cfg.K8s.ConfigMapData(ctx, "valheim-mods")
	if err != nil {
		return shared.SSEToast(c, "err", "Couldn't read installed mods: "+err.Error(), nil)
	}

	set := map[string]string{}
	for _, e := range mods.Parse(data["mods.txt"]) {
		set[pages.ModKey(e)] = EntryVersion(e)
	}

	for _, key := range targetKeys {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			continue
		}
		resolved, err := h.cfg.TS.ResolveTree(ctx, parts[0], parts[1])
		if err != nil {
			return shared.SSEToast(c, "err", "Couldn't resolve "+key+": "+err.Error(), nil)
		}
		for _, r := range resolved {
			set[pages.ModKey(r)] = EntryVersion(r)
		}
	}

	var entries []string
	for k, v := range set {
		entries = append(entries, k+"/"+v)
	}

	changed, err := h.cfg.Mods.Replace(ctx, entries)
	if err != nil {
		return shared.SSEToast(c, "err", "Update commit failed: "+err.Error(), nil)
	}
	if h.cfg.Store != nil {
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "mod-update", strings.Join(targetKeys, ", "))
	}
	if !changed {
		return shared.SSEToast(c, "ok", "Already up to date.", nil)
	}

	want := map[string]bool{}
	for _, e := range entries {
		want[e] = true
	}
	h.cfg.ApplyAfterSync("valheim-mods", "mods.txt", func(txt string) bool {
		have := map[string]bool{}
		for _, e := range mods.Parse(txt) {
			have[e] = true
		}
		for e := range want {
			if !have[e] {
				return false
			}
		}
		return true
	})
	h.SetPending(entries)

	return shared.SSEToast(c, "ok",
		fmt.Sprintf("Updating %d mod(s) — the server will restart once ArgoCD syncs.", len(targetKeys)),
		map[string]any{"selected": map[string]any{}})
}
