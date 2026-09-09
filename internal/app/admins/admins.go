// Package admins manages Valheim admin Steam64 IDs via the StateStore port.
// Applying changes requires a pod restart since ADMINLIST_IDS is rendered on container start.
package admins

import (
	"context"
	"slices"
	"strings"

	"agrelha/internal/ports"
)

type Manager struct {
	store ports.StateStore
	path  string // relPath of valheim-admins.yaml
}

func New(store ports.StateStore, path string) *Manager { return &Manager{store: store, path: path} }

func fields(s string) []string { return strings.Fields(s) }

// Grant adds a Steam64 ID if missing.
func (m *Manager) Grant(ctx context.Context, steamID string) (bool, error) {
	if m.store == nil {
		return false, ports.ErrNotImplemented
	}
	return m.store.Patch(ctx, m.path, "admins: grant "+steamID, func(doc *ports.Document) (bool, error) {
		cur := ""
		if doc.Data != nil {
			cur = doc.Data["ADMINLIST_IDS"]
		}
		if slices.Contains(fields(cur), steamID) {
			return false, nil
		}
		if doc.Data == nil {
			doc.Data = make(map[string]string)
		}
		doc.Data["ADMINLIST_IDS"] = strings.TrimSpace(strings.Join(append(fields(cur), steamID), " "))
		return true, nil
	})
}

// Revoke removes a Steam64 ID.
func (m *Manager) Revoke(ctx context.Context, steamID string) (bool, error) {
	if m.store == nil {
		return false, ports.ErrNotImplemented
	}
	return m.store.Patch(ctx, m.path, "admins: revoke "+steamID, func(doc *ports.Document) (bool, error) {
		cur := ""
		if doc.Data != nil {
			cur = doc.Data["ADMINLIST_IDS"]
		}
		var kept []string
		found := false
		for _, id := range fields(cur) {
			if id != steamID {
				kept = append(kept, id)
			} else {
				found = true
			}
		}
		if !found {
			return false, nil
		}
		if doc.Data == nil {
			doc.Data = make(map[string]string)
		}
		doc.Data["ADMINLIST_IDS"] = strings.Join(kept, " ")
		return true, nil
	})
}
