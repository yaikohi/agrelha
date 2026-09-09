package access

import (
	"context"
	"fmt"
	"strings"

	"agrelha/internal/ports"
)

type AccessManager struct {
	store ports.StateStore
	path  string // relPath of neoforge-access.yaml in yaya-ops
	rcon  ports.Console
}

func NewAccessManager(store ports.StateStore, path string, rcon ports.Console) *AccessManager {
	if path == "" {
		path = "manifests/neoforge-access.yaml"
	}
	return &AccessManager{store: store, path: path, rcon: rcon}
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

// GrantOp adds a user to ops.txt in Git and executes live /op via RCON.
func (a *AccessManager) GrantOp(ctx context.Context, username string) (bool, error) {
	username = strings.TrimSpace(username)
	msg := fmt.Sprintf("mc-access: op %s", username)

	if a.store == nil {
		return false, ports.ErrNotImplemented
	}
	changed, err := a.store.Patch(ctx, a.path, msg, func(doc *ports.Document) (bool, error) {
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
	if err != nil {
		return false, err
	}

	// Live RCON dispatch (best-effort)
	if a.rcon != nil {
		_, _ = a.rcon.Execute("/op " + username)
	}
	return changed, nil
}

// RevokeOp removes a user from ops.txt in Git and executes live /deop via RCON.
func (a *AccessManager) RevokeOp(ctx context.Context, username string) (bool, error) {
	username = strings.TrimSpace(username)
	msg := fmt.Sprintf("mc-access: deop %s", username)

	if a.store == nil {
		return false, ports.ErrNotImplemented
	}
	changed, err := a.store.Patch(ctx, a.path, msg, func(doc *ports.Document) (bool, error) {
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
		if doc.Data == nil {
			doc.Data = make(map[string]string)
		}
		doc.Data["ops.txt"] = strings.Join(lines, "\n")
		return true, nil
	})
	if err != nil {
		return false, err
	}

	if a.rcon != nil {
		_, _ = a.rcon.Execute("/deop " + username)
	}
	return changed, nil
}

// AddWhitelist adds a user to whitelist.txt in Git and executes /whitelist add via RCON.
func (a *AccessManager) AddWhitelist(ctx context.Context, username string) (bool, error) {
	username = strings.TrimSpace(username)
	msg := fmt.Sprintf("mc-access: whitelist add %s", username)

	if a.store == nil {
		return false, ports.ErrNotImplemented
	}
	changed, err := a.store.Patch(ctx, a.path, msg, func(doc *ports.Document) (bool, error) {
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
	if err != nil {
		return false, err
	}

	if a.rcon != nil {
		_, _ = a.rcon.Execute("/whitelist add " + username)
		_, _ = a.rcon.Execute("/whitelist reload")
	}
	return changed, nil
}

// RemoveWhitelist removes a user from whitelist.txt in Git and executes /whitelist remove via RCON.
func (a *AccessManager) RemoveWhitelist(ctx context.Context, username string) (bool, error) {
	username = strings.TrimSpace(username)
	msg := fmt.Sprintf("mc-access: whitelist remove %s", username)

	if a.store == nil {
		return false, ports.ErrNotImplemented
	}
	changed, err := a.store.Patch(ctx, a.path, msg, func(doc *ports.Document) (bool, error) {
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
		if doc.Data == nil {
			doc.Data = make(map[string]string)
		}
		doc.Data["whitelist.txt"] = strings.Join(lines, "\n")
		return true, nil
	})
	if err != nil {
		return false, err
	}

	if a.rcon != nil {
		_, _ = a.rcon.Execute("/whitelist remove " + username)
		_, _ = a.rcon.Execute("/whitelist reload")
	}
	return changed, nil
}

// OnlinePlayers returns the list of online players by querying RCON "/list".
func (a *AccessManager) OnlinePlayers() ([]string, error) {
	if a.rcon == nil {
		return nil, fmt.Errorf("rcon client not configured")
	}
	res, err := a.rcon.Execute("/list")
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
func (a *AccessManager) WhitelistEnforced() (bool, error) {
	if a.rcon == nil {
		return false, fmt.Errorf("rcon client not configured")
	}
	// Modern Minecraft (1.13+) does not have "/whitelist status".
	// Sending "/whitelist on" returns "Whitelist is already turned on" if on,
	// or "Whitelist is now turned on" if off (in which case we immediately revert to off).
	res, err := a.rcon.Execute("/whitelist on")
	if err != nil {
		return false, err
	}
	if strings.Contains(strings.ToLower(res), "already") {
		return true, nil
	}
	// It was off and just got turned on; revert back to off to preserve state.
	_, _ = a.rcon.Execute("/whitelist off")
	return false, nil
}

// SetWhitelistEnforced turns whitelist on or off in-game via RCON.
func (a *AccessManager) SetWhitelistEnforced(enforce bool) error {
	if a.rcon == nil {
		return fmt.Errorf("rcon client not configured")
	}
	cmd := "/whitelist off"
	if enforce {
		cmd = "/whitelist on"
	}
	_, err := a.rcon.Execute(cmd)
	return err
}
