package access

import (
	"testing"

	"agrelha/internal/infra/rcon"
	"agrelha/internal/ports"
)

func TestAccessManagerWithoutConsole(t *testing.T) {
	m := NewAccessManager(nil, "manifests/access.yaml", nil)

	if _, err := m.OnlinePlayers(); err == nil {
		t.Error("OnlinePlayers with no console: want an error, got nil")
	}
	if _, err := m.WhitelistEnforced(); err == nil {
		t.Error("WhitelistEnforced with no console: want an error, got nil")
	}
}

func TestAccessManagerSurvivesTypedNilConsole(t *testing.T) {
	var client *rcon.Client
	var console ports.Console = client

	m := NewAccessManager(nil, "manifests/access.yaml", console)

	if _, err := m.OnlinePlayers(); err == nil {
		t.Error("OnlinePlayers with a typed-nil console: want an error, got nil")
	}
}
