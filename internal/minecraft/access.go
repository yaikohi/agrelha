package minecraft

import (
	"context"
	"fmt"
	"strings"

	"agrelha/internal/gitops"
)

type AccessManager struct {
	committer *gitops.Committer
	path      string      // relPath of neoforge-access.yaml in yaya-ops
	rcon      *RconClient // live RCON client (can be nil if unset)
}

func NewAccessManager(c *gitops.Committer, path string, rcon *RconClient) *AccessManager {
	if path == "" {
		path = "manifests/neoforge-access.yaml"
	}
	return &AccessManager{committer: c, path: path, rcon: rcon}
}

// ParseUsers extracts clean usernames from ops.txt or whitelist.txt.
func ParseUsers(content string) []string {
	var out []string
	for _, line := range strings.Split(content, "\n") {
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

	changed, err := a.committer.Patch(ctx, a.path, "ops.txt", msg, func(cur string) (string, error) {
		for _, u := range ParseUsers(cur) {
			if strings.EqualFold(u, username) {
				return cur, nil
			}
		}
		body := strings.TrimRight(cur, "\n")
		return strings.TrimLeft(body+"\n"+username, "\n") + "\n", nil
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

	changed, err := a.committer.Patch(ctx, a.path, "ops.txt", msg, func(cur string) (string, error) {
		var lines []string
		found := false
		for _, line := range strings.Split(cur, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.EqualFold(trimmed, username) {
				found = true
				continue
			}
			lines = append(lines, line)
		}
		if !found {
			return cur, nil
		}
		return strings.Join(lines, "\n"), nil
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

	changed, err := a.committer.Patch(ctx, a.path, "whitelist.txt", msg, func(cur string) (string, error) {
		for _, u := range ParseUsers(cur) {
			if strings.EqualFold(u, username) {
				return cur, nil
			}
		}
		body := strings.TrimRight(cur, "\n")
		return strings.TrimLeft(body+"\n"+username, "\n") + "\n", nil
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

	changed, err := a.committer.Patch(ctx, a.path, "whitelist.txt", msg, func(cur string) (string, error) {
		var lines []string
		found := false
		for _, line := range strings.Split(cur, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.EqualFold(trimmed, username) {
				found = true
				continue
			}
			lines = append(lines, line)
		}
		if !found {
			return cur, nil
		}
		return strings.Join(lines, "\n"), nil
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

	// Output format typically: "There are X of a max of Y players online: player1, player2"
	parts := strings.Split(res, ":")
	if len(parts) < 2 {
		return nil, nil
	}
	rawPlayers := strings.TrimSpace(parts[1])
	if rawPlayers == "" {
		return nil, nil
	}

	var players []string
	for _, p := range strings.Split(rawPlayers, ",") {
		name := strings.TrimSpace(p)
		if name != "" {
			players = append(players, name)
		}
	}
	return players, nil
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
