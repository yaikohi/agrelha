package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/cmd/web/pages"
	"agrelha/internal/mods"
)

func sseToast(c *fiber.Ctx, kind, msg string, extra map[string]any) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	sig := map[string]any{"toast": msg, "toastkind": kind, "showUpdates": false}
	for k, v := range extra {
		sig[k] = v
	}
	b, _ := json.Marshal(sig)
	return c.SendString(fmt.Sprintf("event: datastar-patch-signals\ndata: signals %s\n\n", string(b)))
}

func (s *FiberServer) modsUpdateAll(c *fiber.Ctx) error {
	var keys []string
	for _, u := range s.modUpdates(c.UserContext()) {
		keys = append(keys, u.Key)
	}
	return s.applyUpdates(c, keys)
}

func (s *FiberServer) modsUpdateSelected(c *fiber.Ctx) error {
	var body struct {
		Selected map[string]bool `json:"selected"`
	}
	_ = json.Unmarshal(c.Body(), &body)

	var keys []string
	for _, u := range s.modUpdates(c.UserContext()) {
		if body.Selected[u.Token] {
			keys = append(keys, u.Key)
		}
	}
	if len(keys) == 0 {
		return sseToast(c, "err", "No mods selected to update.", nil)
	}
	return s.applyUpdates(c, keys)
}

func (s *FiberServer) applyUpdates(c *fiber.Ctx, targetKeys []string) error {
	if s.mods == nil {
		return sseToast(c, "err", "Declarative plane disabled — no Codeberg token configured.", nil)
	}
	if len(targetKeys) == 0 {
		return sseToast(c, "ok", "All mods are already up to date.", nil)
	}
	ctx := c.UserContext()

	if s.pendingActive(ctx) {
		return sseToast(c, "ok", "An update is already in progress — the server will restart once ArgoCD syncs.", nil)
	}

	data, err := s.k8s.ConfigMapData(ctx, "valheim-mods")
	if err != nil {
		return sseToast(c, "err", "Couldn't read installed mods: "+err.Error(), nil)
	}

	set := map[string]string{}
	for _, e := range mods.Parse(data["mods.txt"]) {
		set[pages.ModKey(e)] = entryVersion(e)
	}

	for _, key := range targetKeys {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			continue
		}
		resolved, err := s.ts.ResolveTree(ctx, parts[0], parts[1])
		if err != nil {
			return sseToast(c, "err", "Couldn't resolve "+key+": "+err.Error(), nil)
		}
		for _, r := range resolved {
			set[pages.ModKey(r)] = entryVersion(r)
		}
	}

	var entries []string
	for k, v := range set {
		entries = append(entries, k+"/"+v)
	}

	changed, err := s.mods.Replace(ctx, entries)
	if err != nil {
		return sseToast(c, "err", "Update commit failed: "+err.Error(), nil)
	}
	_ = s.store.RecordAudit(s.actor(c), "mod-update", strings.Join(targetKeys, ", "))
	if !changed {
		return sseToast(c, "ok", "Already up to date.", nil)
	}

	want := map[string]bool{}
	for _, e := range entries {
		want[e] = true
	}
	s.applyAfterSync("valheim-mods", "mods.txt", func(txt string) bool {
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
	s.setPending(entries)

	return sseToast(c, "ok",
		fmt.Sprintf("Updating %d mod(s) — the server will restart once ArgoCD syncs.", len(targetKeys)),
		map[string]any{"selected": map[string]any{}})
}
