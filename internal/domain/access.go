package domain

import (
	"strings"
	"time"
)

// HistoryEntry records an audit action or system event for display and inspection.
type HistoryEntry struct {
	At         time.Time
	Source     string
	Kind       string
	Actor      string
	ActorLabel string
	Detail     string
	Incident   *Incident
}

func (h HistoryEntry) Who() string {
	if h.ActorLabel != "" {
		return h.ActorLabel
	}
	return h.Actor
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
	Instance    int
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

// MinecraftAccess is what an operator granted on a Minecraft World: who
// administers it, who may join, and whether that Admission list is enforced at
// all. Enforcement is deliberately its own field rather than an implication of
// a non-empty whitelist - a World must never become whitelist-only because
// somebody was added to a list, least of all at creation.
//
// WhitelistEnforced is nil when no operator has ever decided: agrelha then says
// nothing about Admission and leaves the server however it was set up.
type MinecraftAccess struct {
	Ops               []string
	Whitelist         []string
	WhitelistEnforced *bool
}

// Enforced reports the decision, treating "never decided" as open.
func (a MinecraftAccess) Enforced() bool {
	return a.WhitelistEnforced != nil && *a.WhitelistEnforced
}
