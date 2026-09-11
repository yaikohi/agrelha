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
}

// Summary is a one-line account of how the server died, for a badge or tile.
func (i Incident) Summary() string {
	switch {
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
