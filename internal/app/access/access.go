package access

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"

	"agrelha/internal/domain"
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

// whitelistEnforcedKey records whether Admission is enforced. Its absence means
// "not enforced": a World that nobody has deliberately closed stays open, which
// is the only safe default - the alternative locks every player out of a World
// the moment it is created.
const whitelistEnforcedKey = "whitelist.enforced"

// DeclaredAccess returns only what agrelha itself holds: the Operators, the
// Admission list and whether it is enforced, as an operator granted them -
// before any reading of what a server currently has. Rendering a server's
// environment from the merged view of ListAccess would feed a server its own
// state back, and a revoke would never take: the name removed here would
// return from the server's own ops.json.
func (a *AccessManager) DeclaredAccess(ctx context.Context) (domain.MinecraftAccess, error) {
	if a == nil || a.store == nil {
		return domain.MinecraftAccess{}, nil
	}
	doc, err := a.store.Get(ctx, a.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, fs.ErrNotExist) {
			return domain.MinecraftAccess{}, nil
		}
		return domain.MinecraftAccess{}, err
	}
	if doc.Data == nil {
		return domain.MinecraftAccess{}, nil
	}
	out := domain.MinecraftAccess{
		Ops:       ParseUsers(doc.Data["ops.txt"]),
		Whitelist: ParseUsers(doc.Data["whitelist.txt"]),
	}
	if raw, ok := doc.Data[whitelistEnforcedKey]; ok {
		enforced := strings.EqualFold(strings.TrimSpace(raw), "true")
		out.WhitelistEnforced = &enforced
	}
	return out, nil
}

// AppliesLive reports whether a change reaches the running server at once. With
// no console it does not, and a page that says otherwise is why an operator
// believes a grant landed when it is only recorded.
func (a *AccessManager) AppliesLive() bool { return a != nil && a.rcon != nil }

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
	if err := domain.ValidateMinecraftUsername(username); err != nil {
		return false, err
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
	if err := domain.ValidateMinecraftUsername(username); err != nil {
		return false, err
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
	if err := domain.ValidateMinecraftUsername(username); err != nil {
		return false, err
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
	if err := domain.ValidateMinecraftUsername(username); err != nil {
		return false, err
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
	declared, err := a.DeclaredAccess(ctx)
	if err != nil {
		return false, err
	}
	return declared.Enforced(), nil
}

// SetWhitelistEnforced records whether Admission is enforced and, when a
// console is reachable, turns it on or off in-game at once. The record is what
// the server is rendered from on its next start, so the switch holds across a
// restart instead of lasting only as long as the process.
func (a *AccessManager) SetWhitelistEnforced(ctx context.Context, enforce bool, actor ...string) error {
	desc := "disabled"
	cmd := "/whitelist off"
	if enforce {
		desc = "enabled"
		cmd = "/whitelist on"
	}

	if a.store == nil && a.rcon == nil {
		return ports.ErrNotImplemented
	}

	if a.store != nil {
		msg := fmt.Sprintf("mc-access: whitelist %s", desc)
		if _, err := a.store.Patch(ctx, a.path, msg, func(doc *ports.Document) (bool, error) {
			if doc.Data == nil {
				doc.Data = make(map[string]string)
			}
			want := "false"
			if enforce {
				want = "true"
			}
			if doc.Data[whitelistEnforcedKey] == want {
				return false, nil
			}
			doc.Data[whitelistEnforcedKey] = want
			return true, nil
		}); err != nil {
			return err
		}
	}

	if a.rcon != nil {
		if _, err := a.rcon.Execute(ctx, cmd); err != nil {
			if a.store == nil {
				return err
			}
			slog.Warn("whitelist switch recorded but the server could not be told now, it applies on its next start",
				"enforced", enforce, "err", err)
		}
	}

	if a.audit != nil {
		_ = a.audit.RecordAudit(actorOrHyphen(actor), "mc-whitelist-toggle", desc)
	}
	return nil
}
