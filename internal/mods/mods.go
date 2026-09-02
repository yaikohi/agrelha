// Package mods edits the valheim-mods ConfigMap's mods.txt (one Thunderstore
// entry per line: namespace/name/version) via a git commit. Comments/blank lines
// are preserved.
package mods

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"agrelha/internal/gitops"
)

type Manager struct {
	c    *gitops.Committer
	path string // relPath of valheim-mods.yaml in the repo
}

func New(c *gitops.Committer, path string) *Manager { return &Manager{c: c, path: path} }

// Parse returns the current mod entries (skipping comments/blanks).
func Parse(content string) []string { return parse(content) }

func parse(content string) []string {
	var out []string
	for _, ln := range strings.Split(content, "\n") {
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
	msg := fmt.Sprintf("mods: install %s", strings.Join(entries, ", "))
	return m.c.Patch(ctx, m.path, "mods.txt", msg, func(cur string) (string, error) {
		present := map[string]bool{}
		for _, e := range parse(cur) {
			present[e] = true
		}
		body := strings.TrimRight(cur, "\n")
		added := 0
		for _, e := range entries {
			e = strings.TrimSpace(e)
			if e == "" || present[e] {
				continue
			}
			body += "\n" + e
			present[e] = true
			added++
		}
		if added == 0 {
			return cur, nil
		}
		return body + "\n", nil
	})
}

// Replace rewrites mods.txt to exactly the given entries (deduped, sorted),
// dropping comments. Used by the update flow, which computes a fresh resolved
// set rather than appending. No-op (returns false) if the set is unchanged.
func (m *Manager) Replace(ctx context.Context, entries []string) (bool, error) {
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
	return m.c.Patch(ctx, m.path, "mods.txt", msg, func(cur string) (string, error) {
		curEntries := parse(cur)
		sort.Strings(curEntries)
		if strings.Join(curEntries, "\n")+"\n" == want {
			return cur, nil
		}
		return want, nil
	})
}

// Remove drops every line whose namespace/name matches (any version).
func (m *Manager) Remove(ctx context.Context, nsName string) (bool, error) {
	msg := "mods: remove " + nsName
	return m.c.Patch(ctx, m.path, "mods.txt", msg, func(cur string) (string, error) {
		var kept []string
		for _, ln := range strings.Split(strings.TrimRight(cur, "\n"), "\n") {
			t := strings.TrimSpace(ln)
			if t != "" && !strings.HasPrefix(t, "#") {
				parts := strings.Split(t, "/")
				if len(parts) >= 2 && parts[0]+"/"+parts[1] == nsName {
					continue // drop
				}
			}
			kept = append(kept, ln)
		}
		return strings.Join(kept, "\n") + "\n", nil
	})
}
