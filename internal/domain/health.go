package domain

import (
	"fmt"
	"time"
)

// Incident is one recorded failure of an Instance: when it died, how it ended,
// and the log tail captured at detection. It is independent of Lifecycle (what
// the operator asked for) and of Availability (whether players can connect now).
type Incident struct {
	ID           int64     `json:"id"`
	GameID       GameID    `json:"game_id"`
	Number       int       `json:"number"`
	At           time.Time `json:"at"`
	RestartCount int32     `json:"restart_count"`
	ExitCode     int32     `json:"exit_code"`
	Reason       string    `json:"reason"`
	OOMKilled    bool      `json:"oom_killed"`
	LogTail      string    `json:"log_tail"`

	// Step names the setup stage that failed when the server never started.
	// Empty means the server ran and then died.
	Step string `json:"step,omitempty"`
}

// Summary is a one-line account of how the server died, for a badge or tile.
func (i Incident) Summary() string {
	switch {
	case i.Step != "":
		return stepSummary(i.Step, i.RestartCount)
	case i.OOMKilled:
		return "Out of memory — the server exceeded its tier's limit"
	case i.Reason == "CrashLoopBackOff":
		return fmt.Sprintf("Crash loop — restarted %d times", i.RestartCount)
	case i.ExitCode != 0:
		return fmt.Sprintf("Exited with code %d", i.ExitCode)
	case i.Reason != "":
		return i.Reason
	default:
		return "Stopped unexpectedly"
	}
}

// stepSummary explains a failure that happened before the server started. The
// operator needs to know it is their mod list at fault, not the game.
func stepSummary(step string, restarts int32) string {
	what := step
	switch step {
	case "mod-reconciler":
		what = "Mod install"
	case "sync-configs":
		what = "Config sync"
	}
	if restarts > 0 {
		return fmt.Sprintf("%s failed — the server never started (%d attempts)", what, restarts)
	}
	return fmt.Sprintf("%s failed — the server never started", what)
}
