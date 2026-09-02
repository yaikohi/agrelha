package server

import (
	"context"
	"sort"
	"strconv"
	"strings"

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
