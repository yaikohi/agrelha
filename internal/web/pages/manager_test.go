package pages

import (
	"strings"
	"testing"
)

func valheimInstance() InstanceUI {
	return InstanceUI{
		IsAdmin: true, CanOperate: true, CanStart: true,
		Number: 2, Name: "Winterhold", LBIP: "10.0.0.2",
		State: "stopped", Tier: "small", MemoryGiB: 4,
	}
}

func labelsOf(actions []ActionUI) []string {
	out := make([]string, 0, len(actions))
	for _, a := range actions {
		out = append(out, a.Label)
	}
	return out
}

func TestManagerOffersNoLifecycleToAViewerWhoCannotOperate(t *testing.T) {
	inst := valheimInstance()
	inst.CanOperate = false
	inst.IsAdmin = false

	port := BuildValheimManager([]InstanceUI{inst}, BudgetUI{}).Ports[0]
	if len(port.Actions) != 0 {
		t.Errorf(`a viewer without a Role for this world was offered %v.

The guard refuses those calls, and a button that always 403s is worse than no button.`, labelsOf(port.Actions))
	}
	if len(port.Links) != 0 {
		t.Errorf("a viewer who cannot operate was offered %v", labelsOf(port.Links))
	}
}

func TestManagerDeleteStaysAdminOnly(t *testing.T) {
	inst := valheimInstance()
	inst.IsAdmin = false

	for _, a := range BuildValheimManager([]InstanceUI{inst}, BudgetUI{}).Ports[0].Actions {
		if a.Label == "Delete" {
			t.Error(`a non-admin was offered Delete.

Deleting a world is admin-only in the guard whoever holds a Role for it.`)
		}
	}

	inst.IsAdmin = true
	if !hasAction(BuildValheimManager([]InstanceUI{inst}, BudgetUI{}).Ports[0].Actions, "Delete") {
		t.Error("an admin must still be able to delete a stopped world")
	}
}

func TestManagerWillNotDeleteARunningWorld(t *testing.T) {
	inst := valheimInstance()
	inst.State = "running"

	actions := BuildValheimManager([]InstanceUI{inst}, BudgetUI{}).Ports[0].Actions
	if hasAction(actions, "Delete") {
		t.Error("Delete was offered on a running world; it must be stopped first")
	}
	if !hasAction(actions, "Stop") {
		t.Errorf("a running world must offer Stop, got %v", labelsOf(actions))
	}
}

func TestManagerExplainsWhyStartIsUnavailable(t *testing.T) {
	inst := valheimInstance()
	inst.CanStart = false
	inst.StartBlockedReason = "two worlds are already running"

	actions := BuildValheimManager([]InstanceUI{inst}, BudgetUI{}).Ports[0].Actions
	var start ActionUI
	for _, a := range actions {
		if a.Label == "Start" {
			start = a
		}
	}
	if start.Kind != "blocked" {
		t.Fatalf("Start kind = %q, want blocked", start.Kind)
	}
	if start.Script != "two worlds are already running" {
		t.Errorf(`blocked Start carries %q.

A disabled control that does not say why leaves the operator guessing.`, start.Script)
	}
}

func TestManagerLampDistinguishesOffFromUnreachable(t *testing.T) {
	cases := []struct {
		name string
		inst InstanceUI
		want string
	}{
		{"players on", InstanceUI{State: "running", PlayersKnown: true, Players: 2}, "lamp lamp-lit"},
		{"running empty", InstanceUI{State: "running", PlayersKnown: true}, "lamp lamp-dim"},
		{"starting", InstanceUI{State: "starting"}, "lamp lamp-dim"},
		{"stopped", InstanceUI{State: "stopped"}, "lamp lamp-out"},
		{"host unreachable", InstanceUI{State: "stopped", HostUnreachable: true}, "lamp lamp-unknown"},
		{"host unknown", InstanceUI{State: "running", HostUnknown: true}, "lamp lamp-unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := managerLamp(c.inst); got != c.want {
				t.Errorf(`managerLamp = %q, want %q.

A world agrelha cannot see must not be drawn as one it knows is off.`, got, c.want)
			}
		})
	}
}

func TestManagerStateLabelMatchesTheStatusVocabulary(t *testing.T) {
	cases := map[string]InstanceUI{
		"Running":             {State: "running"},
		"Stopped":             {State: "stopped"},
		"Starting…":           {State: "starting"},
		"Unknown":             {State: "running", HostUnreachable: true},
		"Host not configured": {State: "running", HostUnknown: true},
	}
	for want, inst := range cases {
		if got := managerStateLabel(inst); got != want {
			t.Errorf("managerStateLabel = %q, want %q", got, want)
		}
	}
}

func TestManagerPortsCopyIndependently(t *testing.T) {
	a := valheimInstance()
	b := valheimInstance()
	b.Number = 3

	ports := BuildValheimManager([]InstanceUI{a, b}, BudgetUI{}).Ports
	if ports[0].CopySignal() == ports[1].CopySignal() {
		t.Fatal("two ports share a copy signal; copying one would confirm on both")
	}
	if !strings.Contains(ports[0].CopyClick(), "agrelhaCopy(") {
		t.Error("the copy helper must be used so plain-http LAN access still copies")
	}
}

func hasAction(actions []ActionUI, label string) bool {
	for _, a := range actions {
		if a.Label == label {
			return true
		}
	}
	return false
}
