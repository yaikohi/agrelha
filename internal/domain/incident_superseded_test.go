package domain

import (
	"testing"
	"time"
)

func TestAFailureBeforeTheCurrentStartIsSuperseded(t *testing.T) {
	crashed := time.Date(2026, 10, 17, 12, 33, 0, 0, time.UTC)
	startedSince := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

	in := Incident{At: crashed, OOMKilled: true, ExitCode: 137}
	if !in.SupersededBy(startedSince) {
		t.Errorf(`a crash on %s is still treated as current even though the server started again on %s.

The failure panel reads the newest incident with no time bound, so a world that crashed once and has
run fine for days still shows "LAST FAILURE" forever. A failure the server has demonstrably restarted
past is history, not a current problem.`, crashed.Format(time.DateOnly), startedSince.Format(time.DateOnly))
	}
}

func TestAFailureSinceTheCurrentStartIsStillCurrent(t *testing.T) {
	startedAt := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	in := Incident{At: startedAt.Add(30 * time.Minute), ExitCode: 1}
	if in.SupersededBy(startedAt) {
		t.Error("a crash that happened after the current start was hidden; that is the one the operator needs to see")
	}
}

func TestAnUnknownStartTimeNeverHidesAFailure(t *testing.T) {
	in := Incident{At: time.Date(2026, 10, 17, 12, 33, 0, 0, time.UTC)}
	if in.SupersededBy(time.Time{}) {
		t.Error(`a zero start time hid the failure.

The runtime reports no start time for a stopped or unreachable world, and "I do not know when it
started" must never be read as "it recovered".`)
	}
}
