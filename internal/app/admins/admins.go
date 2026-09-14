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
	store       ports.StateStore
	path        string // relPath of valheim-admins.yaml
	audit       ports.AuditRecorder
	adminReader func(ctx context.Context) ([]string, error)
}

type Option func(*Manager)

func WithAudit(recorder ports.AuditRecorder) Option {
	return func(m *Manager) {
		m.audit = recorder
	}
}

func WithAdminReader(fn func(ctx context.Context) ([]string, error)) Option {
	return func(m *Manager) {
		m.adminReader = fn
	}
}

func New(store ports.StateStore, path string, opts ...Option) *Manager {
	m := &Manager{store: store, path: path}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func fields(s string) []string { return strings.Fields(s) }

func actorOrHyphen(actor []string) string {
	if len(actor) > 0 && actor[0] != "" {
		return actor[0]
	}
	return "-"
}

// List returns the current list of admin Steam64 IDs.
func (m *Manager) List(ctx context.Context) ([]string, error) {
	if m.adminReader != nil {
		return m.adminReader(ctx)
	}
	if m.store == nil {
		return nil, ports.ErrNotImplemented
	}
	doc, err := m.store.Get(ctx, m.path)
	if err != nil {
		return nil, err
	}
	if doc.Data != nil {
		return fields(doc.Data["ADMINLIST_IDS"]), nil
	}
	return nil, nil
}

// Grant adds a Steam64 ID if missing.
func (m *Manager) Grant(ctx context.Context, steamID string, actor ...string) (bool, error) {
	if m.store == nil {
		return false, ports.ErrNotImplemented
	}
	changed, err := m.store.Patch(ctx, m.path, "admins: grant "+steamID, func(doc *ports.Document) (bool, error) {
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
	if err != nil {
		return false, err
	}
	if changed && m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), "admin-grant", steamID)
	}
	return changed, nil
}

// Revoke removes a Steam64 ID.
func (m *Manager) Revoke(ctx context.Context, steamID string, actor ...string) (bool, error) {
	if m.store == nil {
		return false, ports.ErrNotImplemented
	}
	changed, err := m.store.Patch(ctx, m.path, "admins: revoke "+steamID, func(doc *ports.Document) (bool, error) {
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
		doc.Data["ADMINLIST_IDS"] = strings.Join(kept, " ")
		return true, nil
	})
	if err != nil {
		return false, err
	}
	if changed && m.audit != nil {
		_ = m.audit.RecordAudit(actorOrHyphen(actor), "admin-revoke", steamID)
	}
	return changed, nil
}
