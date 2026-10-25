package docker

import "testing"

func TestAnOperatorStoppingAServerIsNotACrash(t *testing.T) {
	in := ContainerInspect{RestartCount: 0}
	in.State.Status = "exited"
	in.State.ExitCode = 137
	in.State.FinishedAt = "2026-10-24T16:58:46.851350778Z"

	if failureFrom(in).Crashed() {
		t.Error(`a deliberately stopped container was recorded as a crash.

docker stop kills with SIGKILL, so a clean operator-initiated stop also exits 137. Exit code alone
cannot distinguish "the operator pressed Stop" from "the server died", and treating every stop as a
crash would fill the ledger with noise the operator created on purpose.`)
	}
}

func TestARestartedContainerIsACrash(t *testing.T) {
	in := ContainerInspect{RestartCount: 3}
	in.State.Running = true

	f := failureFrom(in)
	if !f.Crashed() {
		t.Error("Docker's restart policy acted three times, which only happens after a failure; that is a crash")
	}
	if f.RestartCount != 3 {
		t.Errorf("restart count lost: got %d", f.RestartCount)
	}
}

func TestAnOutOfMemoryKillIsACrashEvenWithoutARestart(t *testing.T) {
	in := ContainerInspect{RestartCount: 0}
	in.State.OOMKilled = true
	in.State.ExitCode = 137
	in.State.FinishedAt = "2026-10-17T12:33:00Z"

	f := failureFrom(in)
	if !f.Crashed() {
		t.Error("the kernel killed the server for memory; that is the single most important failure to record")
	}
	if !f.OOMKilled {
		t.Error("the out-of-memory flag was dropped, so the operator cannot tell why it died")
	}
	if f.FinishedAt.IsZero() {
		t.Error("the time of death was dropped")
	}
}

func TestAHealthyContainerReportsNoFailure(t *testing.T) {
	in := ContainerInspect{RestartCount: 0}
	in.State.Running = true
	in.State.ExitCode = 0

	if failureFrom(in).Crashed() {
		t.Error("a running container with no restarts was reported as crashed")
	}
}

func TestAZeroFinishedAtIsNotTreatedAsATimestamp(t *testing.T) {
	in := ContainerInspect{RestartCount: 1}
	in.State.FinishedAt = "0001-01-01T00:00:00Z"

	if got := failureFrom(in).FinishedAt; !got.IsZero() {
		t.Errorf(`Docker's "never finished" placeholder was read as a real time: %v.

A container that has never exited reports year 1, and showing that as the moment of failure would
date every crash to the year 1.`, got)
	}
}
