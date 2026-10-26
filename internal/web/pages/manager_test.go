package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

var managers = map[string]func([]InstanceUI, BudgetUI) ManagerUI{
	"valheim":   BuildValheimManager,
	"minecraft": BuildMinecraftManager,
	"gmod":      BuildGModManager,
}

func eachManager(t *testing.T, fn func(t *testing.T, build func([]InstanceUI, BudgetUI) ManagerUI)) {
	t.Helper()
	for game, build := range managers {
		t.Run(game, func(t *testing.T) { fn(t, build) })
	}
}

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
	eachManager(t, func(t *testing.T, build func([]InstanceUI, BudgetUI) ManagerUI) {
		inst := valheimInstance()
		inst.CanOperate = false
		inst.IsAdmin = false

		port := build([]InstanceUI{inst}, BudgetUI{}).Ports[0]
		if len(port.Actions) != 0 {
			t.Errorf(`a viewer without a Role for this world was offered %v.

The guard refuses those calls, and a button that always 403s is worse than no button.`, labelsOf(port.Actions))
		}
		if len(port.Links) != 0 {
			t.Errorf("a viewer who cannot operate was offered %v", labelsOf(port.Links))
		}
	})
}

func TestManagerDeleteStaysAdminOnly(t *testing.T) {
	eachManager(t, func(t *testing.T, build func([]InstanceUI, BudgetUI) ManagerUI) {
		inst := valheimInstance()
		inst.IsAdmin = false

		if hasAction(build([]InstanceUI{inst}, BudgetUI{}).Ports[0].Actions, "Delete") {
			t.Error(`a non-admin was offered Delete.

Deleting a world is admin-only in the guard whoever holds a Role for it.`)
		}

		inst.IsAdmin = true
		if !hasAction(build([]InstanceUI{inst}, BudgetUI{}).Ports[0].Actions, "Delete") {
			t.Error("an admin must still be able to delete a stopped world")
		}
	})
}

func TestManagerWillNotDeleteARunningWorld(t *testing.T) {
	eachManager(t, func(t *testing.T, build func([]InstanceUI, BudgetUI) ManagerUI) {
		inst := valheimInstance()
		inst.State = "running"

		actions := build([]InstanceUI{inst}, BudgetUI{}).Ports[0].Actions
		if hasAction(actions, "Delete") {
			t.Error("Delete was offered on a running world; it must be stopped first")
		}
		if !hasAction(actions, "Stop") {
			t.Errorf("a running world must offer Stop, got %v", labelsOf(actions))
		}
	})
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

func TestEveryManagerPostsToItsOwnGame(t *testing.T) {
	for game, build := range managers {
		t.Run(game, func(t *testing.T) {
			inst := valheimInstance()
			port := build([]InstanceUI{inst}, BudgetUI{}).Ports[0]

			want := "/api/" + game + "/instances/"
			for _, a := range port.Actions {
				if a.Kind == "blocked" {
					continue
				}
				if !strings.Contains(a.Script, want) {
					t.Errorf("%s action %q calls %q, want a %s route", game, a.Label, a.Script, game)
				}
			}
			for _, l := range port.Links {
				if !strings.HasPrefix(l.Href, "/"+game+"/") {
					t.Errorf("%s link %q points at %q", game, l.Label, l.Href)
				}
			}
			if !strings.HasPrefix(port.Href, "/"+game+"/") {
				t.Errorf("%s port links to %q", game, port.Href)
			}
		})
	}
}

func TestDeletePromptNamesTheWorldItWillDestroy(t *testing.T) {
	inst := valheimInstance()
	inst.Name = "Winterhold"

	for game, build := range managers {
		t.Run(game, func(t *testing.T) {
			var del ActionUI
			for _, a := range build([]InstanceUI{inst}, BudgetUI{}).Ports[0].Actions {
				if a.Label == "Delete" {
					del = a
				}
			}
			if !strings.Contains(del.Script, "Winterhold") {
				t.Errorf(`the %s delete prompt does not name the world: %s

A confirm dialog that does not say what it destroys is not a confirmation.`, game, del.Script)
			}
			if !strings.Contains(del.Script, "#02") {
				t.Errorf("the %s delete prompt omits the slot number: %s", game, del.Script)
			}
		})
	}
}

func TestCreateLabelMatchesWhatTheGameCallsAWorld(t *testing.T) {
	cases := map[string]struct {
		build func([]InstanceUI, BudgetUI) ManagerUI
		want  string
	}{
		"valheim":   {BuildValheimManager, "Create world"},
		"minecraft": {BuildMinecraftManager, "Create world"},
		"gmod":      {BuildGModManager, "Create server"},
	}
	for game, c := range cases {
		t.Run(game, func(t *testing.T) {
			m := c.build(nil, BudgetUI{MaxInstances: 4, MaxRunning: 2})
			if m.CreateLabel != c.want {
				t.Errorf("create label = %q, want %q", m.CreateLabel, c.want)
			}
			noun := strings.TrimSuffix(m.CreateLabel, "")
			if !strings.Contains(m.Lede, strings.TrimPrefix(noun, "Create ")+"s") {
				t.Errorf(`the lede %q uses a different noun than the button %q.

Calling the same thing a world in one line and a server in the next makes the
page read like two products.`, m.Lede, m.CreateLabel)
			}
		})
	}
}

func TestManagerActionButtonsCarryInFlightIndicator(t *testing.T) {
	inst := valheimInstance()
	inst.Number = 2
	inst.CanStart = true
	m := BuildValheimManager([]InstanceUI{inst}, BudgetUI{})
	if len(m.Ports) == 0 {
		t.Fatal("expected at least 1 port rendered")
	}

	var buf bytes.Buffer
	if err := managerPort(m.Ports[0]).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "data-indicator=\"busy_2_Start\"") {
		t.Fatalf("manager start button missing instance-scoped indicator busy_2_Start; at 2am an operator receives zero feedback when starting a world and double clicks, causing race conditions, got: %s", out)
	}
	if !strings.Contains(out, "data-attr:disabled=\"$busy_2_Start\"") {
		t.Fatalf("manager start button missing data-attr:disabled binding; at 2am an operator repeatedly clicks the start button while the container boots, got: %s", out)
	}
	if !strings.Contains(out, "Starting…") {
		t.Fatalf("manager start button missing in-flight verb 'Starting…'; at 2am an operator has no visual indication that the request is currently in-flight, got: %s", out)
	}
}

