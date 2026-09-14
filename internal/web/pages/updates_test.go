package pages

import (
	"context"
	"strings"
	"testing"
)

func renderPanel(t *testing.T, d InstanceDetailUI) string {
	t.Helper()
	var b strings.Builder
	if err := ModUpdatePanel(d).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func TestPanelShowsEachPinTransition(t *testing.T) {
	got := renderPanel(t, InstanceDetailUI{
		InstanceUI: InstanceUI{Number: 2, Name: "boppo"},
		ModUpdates: []ModUpdate{{Key: "smoothbrain-mining", FullName: "Smoothbrain-Mining", Current: "1.3.4", Latest: "1.3.9", Token: "smoothbrain_mining"}},
	})
	for _, want := range []string{
		"1 mod update(s) available",
		"Smoothbrain-Mining",
		"1.3.4",
		"1.3.9",
		"selected.smoothbrain_mining",
		"/api/valheim/2/mods/updates/apply",
		"/api/valheim/2/mods/updates/check",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("panel missing %q", want)
		}
	}
}

// The restart is only disruptive when someone is on the world, so that is the
// only time the operator is stopped to confirm it.
func TestConfirmOnlyWhenPlayersWouldBeDropped(t *testing.T) {
	ups := []ModUpdate{{Key: "a-b", FullName: "A-B", Current: "1.0.0", Latest: "1.1.0", Token: "a_b"}}

	cases := []struct {
		name    string
		ui      InstanceUI
		confirm bool
	}{
		{"stopped", InstanceUI{Number: 1, State: "stopped", Players: 3, PlayersKnown: true}, false},
		{"running and empty", InstanceUI{Number: 1, State: "running", Players: 0, PlayersKnown: true}, false},
		{"running, count unknown", InstanceUI{Number: 1, State: "running", Players: 2}, false},
		{"running with players", InstanceUI{Number: 1, State: "running", Players: 2, PlayersKnown: true}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := renderPanel(t, InstanceDetailUI{InstanceUI: c.ui, ModUpdates: ups})
			if strings.Contains(got, "confirm(") != c.confirm {
				t.Errorf("confirm present = %v, want %v", !c.confirm, c.confirm)
			}
		})
	}
}

func TestConfirmNamesThePlayerCount(t *testing.T) {
	got := renderPanel(t, InstanceDetailUI{
		InstanceUI: InstanceUI{Number: 1, Name: "lareira-v2", State: "running", Players: 1, PlayersKnown: true},
		ModUpdates: []ModUpdate{{Key: "a-b", FullName: "A-B", Current: "1.0.0", Latest: "1.1.0", Token: "a_b"}},
	})
	if !strings.Contains(got, "1 player is connected to lareira-v2") {
		t.Errorf("confirmation does not name who it would drop: %s", got)
	}
}

func TestPanelSaysUpToDateWithoutButtons(t *testing.T) {
	got := renderPanel(t, InstanceDetailUI{
		InstanceUI:     InstanceUI{Number: 3},
		UpdatesChecked: "4m",
		ModsChecked:    16,
		ModsTotal:      16,
	})
	if !strings.Contains(got, "Mods are up to date") {
		t.Error("want the up-to-date state")
	}
	if strings.Contains(got, "updates/apply") {
		t.Error("nothing to apply, so the apply buttons must not render")
	}
	if !strings.Contains(got, "Checked all 16 mods, 4m ago") {
		t.Errorf("want the last-checked summary, got: %s", got)
	}
}

func TestPanelShowsPendingRollout(t *testing.T) {
	got := renderPanel(t, InstanceDetailUI{
		InstanceUI:     InstanceUI{Number: 1},
		ModUpdates:     []ModUpdate{{Key: "a-b", FullName: "A-B", Current: "1.0.0", Latest: "1.1.0", Token: "a_b"}},
		UpdatesPending: true,
	})
	if !strings.Contains(got, "restarts once ArgoCD syncs") {
		t.Error("want the pending-rollout banner")
	}
	if !strings.Contains(got, "disabled") {
		t.Error("apply buttons must be disabled while an update is already landing")
	}
}

// "Up to date", "couldn't ask" and "no longer published" must never render the
// same. A stale index once reported the third as the first.
func TestPanelSeparatesUpToDateFromUnanswered(t *testing.T) {
	got := renderPanel(t, InstanceDetailUI{
		InstanceUI:      InstanceUI{Number: 2},
		ModsUnreachable: []string{"Flaky-Thing"},
		ModsChecked:     15,
		ModsTotal:       16,
		UpdatesChecked:  "2m",
	})
	if !strings.Contains(got, "Checked 15 of 16 mods, 2m ago") {
		t.Errorf("want the partial-check summary, got: %s", got)
	}
	if !strings.Contains(got, "Flaky-Thing") {
		t.Error("want the unreachable mod named")
	}
	if !strings.Contains(got, "Couldn't reach Thunderstore") {
		t.Error("want the unreachable notice")
	}
}

func TestPanelWarnsAboutModsGoneFromThunderstore(t *testing.T) {
	got := renderPanel(t, InstanceDetailUI{
		InstanceUI:     InstanceUI{Number: 2},
		ModsMissing:    []string{"Deleted-Package", "Gone-Thing"},
		ModsChecked:    14,
		ModsTotal:      16,
		UpdatesChecked: "2m",
	})
	for _, want := range []string{
		"2 installed mod(s) are no longer published on Thunderstore",
		"fail to boot on its next restart",
		"Deleted-Package, Gone-Thing",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("panel missing %q", want)
		}
	}
}

func TestPanelSurfacesACheckThatFailedOutright(t *testing.T) {
	got := renderPanel(t, InstanceDetailUI{
		InstanceUI:   InstanceUI{Number: 2},
		UpdatesError: "Last check failed: connection refused",
	})
	if !strings.Contains(got, "Last check failed: connection refused") {
		t.Error("a failed check must say so rather than claiming everything is fine")
	}
}

func TestUndoOnlyOfferedWhenThereIsAWayBack(t *testing.T) {
	without := renderPanel(t, InstanceDetailUI{InstanceUI: InstanceUI{Number: 2}})
	if strings.Contains(without, "Undo last update") {
		t.Error("no restore point, so no undo button")
	}

	with := renderPanel(t, InstanceDetailUI{
		InstanceUI: InstanceUI{Number: 2},
		CanUndo:    true,
		UndoWhen:   "8m ago",
	})
	if !strings.Contains(with, "Undo last update") {
		t.Error("want the undo button")
	}
	if !strings.Contains(with, "/api/valheim/2/mods/updates/undo") {
		t.Error("want the undo route")
	}
	if !strings.Contains(with, "8m ago") {
		t.Error("want the restore point's age in the tooltip")
	}
}

func TestUndoConfirmsWhenPlayersWouldBeDropped(t *testing.T) {
	got := renderPanel(t, InstanceDetailUI{
		InstanceUI: InstanceUI{Number: 2, Name: "boppo", State: "running", Players: 2, PlayersKnown: true},
		CanUndo:    true,
		UndoWhen:   "8m ago",
	})
	if !strings.Contains(got, "2 players are connected to boppo. Reverting restarts the world") {
		t.Errorf("undo must warn like apply does, got: %s", got)
	}
}
