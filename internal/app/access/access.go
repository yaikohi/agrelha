package access

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"agrelha/internal/ports"
)

type AccessManager struct {
	store        ports.StateStore
	path         string // relPath of neoforge-access.yaml in yaya-ops
	rcon         ports.Console
	audit        ports.AuditRecorder
	accessReader func(ctx context.Context) ([]string, []string, error)
}

type AccessOption func(*AccessManager)

func WithAudit(recorder ports.AuditRecorder) AccessOption {
	return func(a *AccessManager) {
		a.audit = recorder
	}
}

func WithAccessReader(fn func(ctx context.Context) ([]string, []string, error)) AccessOption {
	return func(a *AccessManager) {
		a.accessReader = fn
	}
}

func NewAccessManager(store ports.StateStore, path string, rcon ports.Console, opts ...AccessOption) *AccessManager {
	if path == "" {
		path = "manifests/neoforge-access.yaml"
	}
	a := &AccessManager{store: store, path: path, rcon: rcon}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// ListAccess returns the current operator usernames and whitelisted usernames.
func (a *AccessManager) ListAccess(ctx context.Context) (ops []string, whitelist []string, err error) {
	if a.accessReader != nil {
		return a.accessReader(ctx)
	}
	if a.store == nil && a.rcon == nil {
		return nil, nil, ports.ErrNotImplemented
	}
	var storeErr error
	if a.store != nil {
		doc, err := a.store.Get(ctx, a.path)
		if err == nil {
			if doc.Data != nil {
				ops = ParseUsers(doc.Data["ops.txt"])
				whitelist = ParseUsers(doc.Data["whitelist.txt"])
			}
			return ops, whitelist, nil
		}
		storeErr = err
	}
	if a.rcon != nil {
		if res, err := a.rcon.Execute(ctx, "/whitelist list"); err == nil {
			whitelist = ParsePlayerList(res)
			return ops, whitelist, nil
		}
	}
	if a.store == nil || errors.Is(storeErr, os.ErrNotExist) {
		return nil, nil, nil
	}
	return nil, nil, storeErr
}

// ParseUsers extracts clean usernames from ops.txt or whitelist.txt.
func ParseUsers(content string) []string {
	var out []string
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func actorOrHyphen(actor []string) string {
	if len(actor) > 0 && actor[0] != "" {
		return actor[0]
	}
	return "-"
}

// GrantOp adds a user to ops.txt in Git and executes live /op via RCON.
func (a *AccessManager) GrantOp(ctx context.Context, username string, actor ...string) (bool, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return false, fmt.Errorf("username is required")
	}
	msg := fmt.Sprintf("mc-access: op %s", username)

	var changed bool
	var storeErr error
	if a.store != nil {
		changed, storeErr = a.store.Patch(ctx, a.path, msg, func(doc *ports.Document) (bool, error) {
			cur := ""
			if doc.Data != nil {
				cur = doc.Data["ops.txt"]
			}
			for _, u := range ParseUsers(cur) {
				if strings.EqualFold(u, username) {
					return false, nil
				}
			}
			body := strings.TrimRight(cur, "\n")
			if doc.Data == nil {
				doc.Data = make(map[string]string)
			}
			doc.Data["ops.txt"] = strings.TrimLeft(body+"\n"+username, "\n") + "\n"
			return true, nil
		})
	}

	var rconErr error
	if a.rcon != nil {
		res, err := a.rcon.Execute(ctx, "/op "+username)
		if err != nil {
			rconErr = err
		} else {
			if strings.Contains(strings.ToLower(res), "already") {
				changed = false
			} else if a.store == nil || storeErr != nil {
				changed = true
			}
		}
	}

	if a.store == nil && a.rcon == nil {
		return false, ports.ErrNotImplemented
	}
	if storeErr != nil && (a.rcon == nil || rconErr != nil) {
		if rconErr != nil {
			return false, fmt.Errorf("store patch: %v; rcon: %w", storeErr, rconErr)
		}
		return false, storeErr
	}
	if a.store == nil && rconErr != nil {
		return false, rconErr
	}

	if changed && a.audit != nil {
		_ = a.audit.RecordAudit(actorOrHyphen(actor), "mc-op-grant", username)
	}
	return changed, nil
}

// RevokeOp removes a user from ops.txt in Git and executes live /deop via RCON.
func (a *AccessManager) RevokeOp(ctx context.Context, username string, actor ...string) (bool, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return false, fmt.Errorf("username is required")
	}
	msg := fmt.Sprintf("mc-access: deop %s", username)

	var changed bool
	var storeErr error
	if a.store != nil {
		changed, storeErr = a.store.Patch(ctx, a.path, msg, func(doc *ports.Document) (bool, error) {
			cur := ""
			if doc.Data != nil {
				cur = doc.Data["ops.txt"]
			}
			var lines []string
			found := false
			for line := range strings.SplitSeq(cur, "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.EqualFold(trimmed, username) {
					found = true
					continue
				}
				lines = append(lines, line)
			}
			if !found {
				return false, nil
			}
			doc.Data["ops.txt"] = strings.Join(lines, "\n")
			return true, nil
		})
	}

	var rconErr error
	if a.rcon != nil {
		res, err := a.rcon.Execute(ctx, "/deop "+username)
		if err != nil {
			rconErr = err
		} else {
			if strings.Contains(strings.ToLower(res), "not an op") {
				changed = false
			} else if a.store == nil || storeErr != nil {
				changed = true
			}
		}
	}

	if a.store == nil && a.rcon == nil {
		return false, ports.ErrNotImplemented
	}
	if storeErr != nil && (a.rcon == nil || rconErr != nil) {
		if rconErr != nil {
			return false, fmt.Errorf("store patch: %v; rcon: %w", storeErr, rconErr)
		}
		return false, storeErr
	}
	if a.store == nil && rconErr != nil {
		return false, rconErr
	}

	if changed && a.audit != nil {
		_ = a.audit.RecordAudit(actorOrHyphen(actor), "mc-op-revoke", username)
	}
	return changed, nil
}

// AddWhitelist adds a user to whitelist.txt in Git and executes /whitelist add via RCON.
func (a *AccessManager) AddWhitelist(ctx context.Context, username string, actor ...string) (bool, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return false, fmt.Errorf("username is required")
	}
	msg := fmt.Sprintf("mc-access: whitelist add %s", username)

	var changed bool
	var storeErr error
	if a.store != nil {
		changed, storeErr = a.store.Patch(ctx, a.path, msg, func(doc *ports.Document) (bool, error) {
			cur := ""
			if doc.Data != nil {
				cur = doc.Data["whitelist.txt"]
			}
			for _, u := range ParseUsers(cur) {
				if strings.EqualFold(u, username) {
					return false, nil
				}
			}
			body := strings.TrimRight(cur, "\n")
			if doc.Data == nil {
				doc.Data = make(map[string]string)
			}
			doc.Data["whitelist.txt"] = strings.TrimLeft(body+"\n"+username, "\n") + "\n"
			return true, nil
		})
	}

	var rconErr error
	if a.rcon != nil {
		res, err := a.rcon.Execute(ctx, "/whitelist add "+username)
		if err != nil {
			rconErr = err
		} else {
			if strings.Contains(strings.ToLower(res), "already") {
				changed = false
			} else if a.store == nil || storeErr != nil {
				changed = true
			}
			_, _ = a.rcon.Execute(ctx, "/whitelist reload")
		}
	}

	if a.store == nil && a.rcon == nil {
		return false, ports.ErrNotImplemented
	}
	if storeErr != nil && (a.rcon == nil || rconErr != nil) {
		if rconErr != nil {
			return false, fmt.Errorf("store patch: %v; rcon: %w", storeErr, rconErr)
		}
		return false, storeErr
	}
	if a.store == nil && rconErr != nil {
		return false, rconErr
	}

	if changed && a.audit != nil {
		_ = a.audit.RecordAudit(actorOrHyphen(actor), "mc-whitelist-add", username)
	}
	return changed, nil
}

// RemoveWhitelist removes a user from whitelist.txt in Git and executes /whitelist remove via RCON.
func (a *AccessManager) RemoveWhitelist(ctx context.Context, username string, actor ...string) (bool, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return false, fmt.Errorf("username is required")
	}
	msg := fmt.Sprintf("mc-access: whitelist remove %s", username)

	var changed bool
	var storeErr error
	if a.store != nil {
		changed, storeErr = a.store.Patch(ctx, a.path, msg, func(doc *ports.Document) (bool, error) {
			cur := ""
			if doc.Data != nil {
				cur = doc.Data["whitelist.txt"]
			}
			var lines []string
			found := false
			for line := range strings.SplitSeq(cur, "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.EqualFold(trimmed, username) {
					found = true
					continue
				}
				lines = append(lines, line)
			}
			if !found {
				return false, nil
			}
			doc.Data["whitelist.txt"] = strings.Join(lines, "\n")
			return true, nil
		})
	}

	var rconErr error
	if a.rcon != nil {
		res, err := a.rcon.Execute(ctx, "/whitelist remove "+username)
		if err != nil {
			rconErr = err
		} else {
			if strings.Contains(strings.ToLower(res), "not whitelisted") {
				changed = false
			} else if a.store == nil || storeErr != nil {
				changed = true
			}
			_, _ = a.rcon.Execute(ctx, "/whitelist reload")
		}
	}

	if a.store == nil && a.rcon == nil {
		return false, ports.ErrNotImplemented
	}
	if storeErr != nil && (a.rcon == nil || rconErr != nil) {
		if rconErr != nil {
			return false, fmt.Errorf("store patch: %v; rcon: %w", storeErr, rconErr)
		}
		return false, storeErr
	}
	if a.store == nil && rconErr != nil {
		return false, rconErr
	}

	if changed && a.audit != nil {
		_ = a.audit.RecordAudit(actorOrHyphen(actor), "mc-whitelist-remove", username)
	}
	return changed, nil
}

// OnlinePlayers returns the list of online players by querying RCON "/list".
func (a *AccessManager) OnlinePlayers(ctx context.Context) ([]string, error) {
	if a.rcon == nil {
		return nil, fmt.Errorf("rcon client not configured")
	}
	res, err := a.rcon.Execute(ctx, "/list")
	if err != nil {
		return nil, err
	}

	return ParsePlayerList(res), nil
}

// ParsePlayerList extracts player names from an RCON "/list" response, which
// reads "There are X of a max of Y players online: player1, player2".
func ParsePlayerList(res string) []string {
	parts := strings.Split(res, ":")
	if len(parts) < 2 {
		return nil
	}
	raw := strings.TrimSpace(parts[1])
	if raw == "" {
		return nil
	}

	var players []string
	for p := range strings.SplitSeq(raw, ",") {
		if name := strings.TrimSpace(p); name != "" {
			players = append(players, name)
		}
	}
	return players
}

// WhitelistEnforced checks if the whitelist is currently enabled in-game via RCON.
func (a *AccessManager) WhitelistEnforced(ctx context.Context) (bool, error) {
	if a.rcon == nil {
		return false, fmt.Errorf("rcon client not configured")
	}
	// Modern Minecraft (1.13+) does not have "/whitelist status".
	// Sending "/whitelist on" returns "Whitelist is already turned on" if on,
	// or "Whitelist is now turned on" if off (in which case we immediately revert to off).
	res, err := a.rcon.Execute(ctx, "/whitelist on")
	if err != nil {
		return false, err
	}
	if strings.Contains(strings.ToLower(res), "already") {
		return true, nil
	}
	// It was off and just got turned on; revert back to off to preserve state.
	_, _ = a.rcon.Execute(ctx, "/whitelist off")
	return false, nil
}

// SetWhitelistEnforced turns whitelist on or off in-game via RCON.
func (a *AccessManager) SetWhitelistEnforced(ctx context.Context, enforce bool, actor ...string) error {
	if a.rcon == nil {
		return fmt.Errorf("rcon client not configured")
	}
	cmd := "/whitelist off"
	desc := "disabled"
	if enforce {
		cmd = "/whitelist on"
		desc = "enabled"
	}
	_, err := a.rcon.Execute(ctx, cmd)
	if err != nil {
		return err
	}
	if a.audit != nil {
		_ = a.audit.RecordAudit(actorOrHyphen(actor), "mc-whitelist-toggle", desc)
	}
	return nil
}
