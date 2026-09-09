package server

import (
	"context"
	"time"

	"agrelha/internal/app/mods"
	contenthttp "agrelha/internal/web/handlers/content"
	"agrelha/internal/web/pages"
)

func entryVersion(entry string) string {
	return contenthttp.EntryVersion(entry)
}

const pendingTTL = contenthttp.PendingTTL

func versionNewer(a, b string) bool {
	return contenthttp.VersionNewer(a, b)
}

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
	return s.ensureContentHandler().ModUpdates(ctx)
}
