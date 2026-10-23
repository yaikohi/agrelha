package components

import (
	"strings"
	"testing"
)

func TestThePillNeverClaimsRunningForAHostItCannotSee(t *testing.T) {
	p := StatusPillProps{State: "running", HostUnreachable: true}

	if got := p.displayLabel(); got != "Unknown" {
		t.Errorf(`a world on an unreachable Host rendered %q.

State is the last value agrelha saw, not what is true now, so showing "Running" tells the
operator a world is up when nothing has confirmed that since the Host went away. The one
indicator you would check to notice a dead Host must not be the one that lies.`, got)
	}
	if strings.Contains(p.pillClasses(), "emerald") {
		t.Error("the pill kept its healthy green styling while the Host was unreachable")
	}
	if strings.Contains(p.dotClasses(), "animate-pulse") {
		t.Error("the pill kept its live pulse animation while the Host was unreachable")
	}
}

func TestThePillSeparatesAMisconfiguredHostFromASleepingOne(t *testing.T) {
	unknown := StatusPillProps{State: "running", HostUnknown: true}

	if got := unknown.displayLabel(); got != "Host not configured" {
		t.Errorf(`a world naming a Host no runtime serves rendered %q.

Waiting for an unreachable machine and fixing a Host nobody serves are different remedies,
so they must not render the same. Reporting this as "Unknown" sends the operator to wait for
a Host that is never coming back.`, got)
	}
	if !strings.Contains(unknown.pillClasses(), "red") {
		t.Errorf("a misconfigured Host did not render as an error; classes were %q", unknown.pillClasses())
	}
	if unknown.displayLabel() == (StatusPillProps{State: "running", HostUnreachable: true}).displayLabel() {
		t.Error("a misconfigured Host and an unreachable Host render identically")
	}
}

func TestTheHostFlagsDoNotDisturbAHealthyPill(t *testing.T) {
	p := StatusPillProps{State: "running"}
	if got := p.displayLabel(); got != "Running" {
		t.Errorf("a reachable running world rendered %q, want %q", got, "Running")
	}
	if !strings.Contains(p.pillClasses(), "emerald") {
		t.Error("a reachable running world lost its healthy styling")
	}
	if got := (StatusPillProps{State: "stopped"}).displayLabel(); got != "Stopped" {
		t.Errorf("a stopped world rendered %q", got)
	}
}
