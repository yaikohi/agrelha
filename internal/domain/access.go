package domain

import (
	"strings"
	"time"
)

// HistoryEntry records an audit action or system event for display and inspection.
type HistoryEntry struct {
	At     time.Time
	Source string
	Kind   string
	Actor  string
	Detail string
}

// Player represents a player in the Valheim roster and their connection state.
type Player struct {
	SteamID     string
	Character   string
	FirstSeen   time.Time
	LastSeen    time.Time
	Sessions    int
	Online      bool
	OnlineSince time.Time
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

// Fields splits a whitespace-separated list of IDs.
func Fields(s string) []string {
	return strings.Fields(s)
}
