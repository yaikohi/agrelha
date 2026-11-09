package access

import (
	"context"
	"testing"

	"agrelha/internal/infra/rcon"
	"agrelha/internal/ports"
)

func TestAccessManagerWithoutConsole(t *testing.T) {
	m := NewAccessManager(nil, "manifests/access.yaml", nil)

	if _, err := m.OnlinePlayers(context.Background()); err == nil {
		t.Error("OnlinePlayers with no console: want an error, got nil")
	}
	// Admission is read from what agrelha recorded, so a missing console is not
	// an error - it only means nothing reaches the server until it restarts.
	if enforced, err := m.WhitelistEnforced(context.Background()); err != nil || enforced {
		t.Errorf("WhitelistEnforced with no console: enforced=%v err=%v, want false and no error", enforced, err)
	}
	if m.AppliesLive() {
		t.Error("a manager with no console claimed changes apply in-game now")
	}
}

func TestAccessManagerSurvivesTypedNilConsole(t *testing.T) {
	var client *rcon.Client
	var console ports.Console = client

	m := NewAccessManager(nil, "manifests/access.yaml", console)

	if _, err := m.OnlinePlayers(context.Background()); err == nil {
		t.Error("OnlinePlayers with a typed-nil console: want an error, got nil")
	}
}
