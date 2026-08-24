// Package admins edits the valheim-admins ConfigMap's ADMINLIST_IDS (space-
// separated Steam64 IDs) via a git commit. Applying it needs a pod restart
// (the caller triggers that), since ADMINLIST_IDS is rendered on container start.
package admins

import (
	"context"
	"strings"

	"agrelha/internal/gitops"
)

type Manager struct {
	c    *gitops.Committer
	path string // relPath of valheim-admins.yaml
}

func New(c *gitops.Committer, path string) *Manager { return &Manager{c: c, path: path} }

func fields(s string) []string { return strings.Fields(s) }

// Grant adds a Steam64 ID if missing.
func (m *Manager) Grant(ctx context.Context, steamID string) (bool, error) {
	return m.c.Patch(ctx, m.path, "ADMINLIST_IDS", "admins: grant "+steamID,
		func(cur string) (string, error) {
			for _, id := range fields(cur) {
				if id == steamID {
					return cur, nil
				}
			}
			return strings.TrimSpace(strings.Join(append(fields(cur), steamID), " ")), nil
		})
}

// Revoke removes a Steam64 ID.
func (m *Manager) Revoke(ctx context.Context, steamID string) (bool, error) {
	return m.c.Patch(ctx, m.path, "ADMINLIST_IDS", "admins: revoke "+steamID,
		func(cur string) (string, error) {
			var kept []string
			for _, id := range fields(cur) {
				if id != steamID {
					kept = append(kept, id)
				}
			}
			return strings.Join(kept, " "), nil
		})
}
