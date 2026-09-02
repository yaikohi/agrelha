package server

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"agrelha/cmd/web/pages"
	"agrelha/internal/mods"
)

func entryVersion(entry string) string {
	p := strings.Split(strings.TrimSpace(entry), "/")
	if len(p) < 3 {
		return ""
	}
	return p[len(p)-1]
}

func versionNewer(a, b string) bool {
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

const pendingTTL = 6 * time.Minute

// setPending records that a resolved set was just committed and is waiting for
// ArgoCD to reconcile + the pod to roll. pendingActive clears itself once the
// live ConfigMap reflects the set (or the TTL lapses, so a stuck sync doesn't
// leave the UI in "updating" forever).
func (s *FiberServer) setPending(entries []string) {
	set := make(map[string]bool, len(entries))
	for _, e := range entries {
		set[e] = true
	}
	s.pendMu.Lock()
	s.pendSet = set
	s.pendAt = time.Now()
	s.pendMu.Unlock()
}

func (s *FiberServer) pendingActive(ctx context.Context) bool {
	s.pendMu.Lock()
	set, at := s.pendSet, s.pendAt
	s.pendMu.Unlock()
	if set == nil {
		return false
	}
	if time.Since(at) > pendingTTL {
		s.clearPending()
		return false
	}
	if s.k8s == nil {
		return true
	}
	data, err := s.k8s.ConfigMapData(ctx, "valheim-mods")
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
	s.clearPending()
	return false
}

func (s *FiberServer) clearPending() {
	s.pendMu.Lock()
	s.pendSet = nil
	s.pendMu.Unlock()
}

func (s *FiberServer) modUpdates(ctx context.Context) []pages.ModUpdate {
	if s.k8s == nil {
		return nil
	}
	data, err := s.k8s.ConfigMapData(ctx, "valheim-mods")
	if err != nil {
		return nil
	}
	var out []pages.ModUpdate
	for _, e := range mods.Parse(data["mods.txt"]) {
		key := pages.ModKey(e)
		cur := entryVersion(e)
		m, ok := s.ts.Get(key)
		if !ok || m.Version == "" || cur == "" {
			continue
		}
		if versionNewer(m.Version, cur) {
			out = append(out, pages.ModUpdate{Key: key, Current: cur, Latest: m.Version, Token: pages.UpdateToken(key)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
