// Package mods edits the valheim-mods ConfigMap's mods.txt (one Thunderstore
// entry per line: namespace/name/version) via the StateStore port.
// Comments/blank lines are preserved.
package mods

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"agrelha/internal/ports"
)

type Manager struct {
	store     ports.StateStore
	path      string // relPath of valheim-mods.yaml in the repo
	modReader func(ctx context.Context) ([]string, error)
}

type Option func(*Manager)

// WithModReader configures an optional live reader for installed mods (e.g. from a live cluster ConfigMap).
func WithModReader(fn func(ctx context.Context) ([]string, error)) Option {
	return func(m *Manager) {
		m.modReader = fn
	}
}

func New(store ports.StateStore, path string, opts ...Option) *Manager {
	m := &Manager{store: store, path: path}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// InstalledMods returns the list of currently installed mods either from the configured modReader or the StateStore.
func (m *Manager) InstalledMods(ctx context.Context) ([]string, error) {
	if m == nil {
		return nil, nil
	}
	if m.modReader != nil {
		return m.modReader(ctx)
	}
	if m.store == nil {
		return nil, ports.ErrNotImplemented
	}
	doc, err := m.store.Get(ctx, m.path)
	if err != nil {
		return nil, err
	}
	if doc.Data == nil {
		return nil, nil
	}
	return parse(doc.Data["mods.txt"]), nil
}

// Parse returns the current mod entries (skipping comments/blanks).
func Parse(content string) []string { return parse(content) }

func parse(content string) []string {
	var out []string
	for ln := range strings.SplitSeq(content, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		out = append(out, t)
	}
	return out
}

// Install appends the given entries (namespace/name/version) that aren't already
// present, preserving existing lines/comments. entries should already include
// resolved dependencies.
func (m *Manager) Install(ctx context.Context, entries []string) (bool, error) {
	if m.store == nil {
		return false, ports.ErrNotImplemented
	}
	msg := fmt.Sprintf("mods: install %s", strings.Join(entries, ", "))
	return m.store.Patch(ctx, m.path, msg, func(doc *ports.Document) (bool, error) {
		cur := ""
		if doc.Data != nil {
			cur = doc.Data["mods.txt"]
		}
		present := map[string]bool{}
		for _, e := range parse(cur) {
			present[e] = true
		}
		var body strings.Builder
		body.WriteString(strings.TrimRight(cur, "\n"))
		added := 0
		for _, e := range entries {
			e = strings.TrimSpace(e)
			if e == "" || present[e] {
				continue
			}
			body.WriteString("\n" + e)
			present[e] = true
			added++
		}
		if added == 0 {
			return false, nil
		}
		if doc.Data == nil {
			doc.Data = make(map[string]string)
		}
		doc.Data["mods.txt"] = body.String() + "\n"
		return true, nil
	})
}

// Replace rewrites mods.txt to exactly the given entries (deduped, sorted),
// dropping comments. Used by the update flow, which computes a fresh resolved
// set rather than appending. No-op (returns false) if the set is unchanged.
func (m *Manager) Replace(ctx context.Context, entries []string) (bool, error) {
	if m.store == nil {
		return false, ports.ErrNotImplemented
	}
	seen := map[string]bool{}
	var uniq []string
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		uniq = append(uniq, e)
	}
	sort.Strings(uniq)
	want := strings.Join(uniq, "\n") + "\n"
	msg := "mods: update to latest"
	return m.store.Patch(ctx, m.path, msg, func(doc *ports.Document) (bool, error) {
		cur := ""
		if doc.Data != nil {
			cur = doc.Data["mods.txt"]
		}
		curEntries := parse(cur)
		sort.Strings(curEntries)
		if strings.Join(curEntries, "\n")+"\n" == want {
			return false, nil
		}
		if doc.Data == nil {
			doc.Data = make(map[string]string)
		}
		doc.Data["mods.txt"] = want
		return true, nil
	})
}

// Remove drops every line whose namespace/name matches (any version).
func (m *Manager) Remove(ctx context.Context, nsName string) (bool, error) {
	if m.store == nil {
		return false, ports.ErrNotImplemented
	}
	msg := "mods: remove " + nsName
	return m.store.Patch(ctx, m.path, msg, func(doc *ports.Document) (bool, error) {
		cur := ""
		if doc.Data != nil {
			cur = doc.Data["mods.txt"]
		}
		var kept []string
		found := false
		for ln := range strings.SplitSeq(strings.TrimRight(cur, "\n"), "\n") {
			t := strings.TrimSpace(ln)
			if t != "" && !strings.HasPrefix(t, "#") {
				parts := strings.Split(t, "/")
				if len(parts) >= 2 && parts[0]+"/"+parts[1] == nsName {
					found = true
					continue // drop
				}
			}
			kept = append(kept, ln)
		}
		if !found {
			return false, nil
		}
		doc.Data["mods.txt"] = strings.Join(kept, "\n") + "\n"
		return true, nil
	})
}
