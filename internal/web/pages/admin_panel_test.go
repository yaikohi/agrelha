package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func adminSample() []DashboardGameUI {
	return []DashboardGameUI{
		{
			Card: GameCardUI{Title: "Valheim Worlds", Accent: "orange", Extras: []string{"1 of 4 worlds saved"}, Rows: []ServerRowUI{
				{Name: "Ducktopia", Address: "10.0.0.1:2456", Online: true},
			}},
			Actions: CardActionsUI{
				Links:   []ActionUI{{Label: "Access", Href: "/access?game=valheim"}},
				Special: []ActionUI{{Label: "+ Create World", Href: "/valheim/create"}},
				Manager: ActionUI{Label: "Open Valheim Manager →", Href: "/valheim"},
			},
		},
		{
			Card: GameCardUI{Title: "Minecraft Worlds", Accent: "emerald", EmptyHint: "ask the host on Discord"},
			Actions: CardActionsUI{
				Manager: ActionUI{Label: "Open Minecraft Manager →", Href: "/minecraft"},
			},
		},
	}
}

func TestAdminPanelKeepsPortIndexesUniqueAcrossGames(t *testing.T) {
	games := adminSample()
	games[1].Card.Rows = []ServerRowUI{
		{Name: "Create Live", Address: "10.0.0.2:25565"},
		{Name: "Vanilla", Address: "10.0.0.3:25565"},
	}

	seen := map[int]bool{}
	for _, section := range BuildAdminPanel(games) {
		for _, port := range section.Ports {
			if seen[port.Index] {
				t.Fatalf("port index %d reused; two ports would share a copy signal", port.Index)
			}
			seen[port.Index] = true
		}
	}
	if len(seen) != 3 {
		t.Fatalf("indexed %d ports, want 3", len(seen))
	}
}

func TestAdminPanelDropsTheVisitorFacingHint(t *testing.T) {
	sections := BuildAdminPanel(adminSample())
	idle := sections[1].Idle
	if idle == nil {
		t.Fatal("a game with no rows must still render an idle port")
	}
	if idle.Hint != "" {
		t.Errorf(`idle hint = %q.

The hub is the host's own page: telling them to ask the host on Discord is
addressed to the wrong person. The create action below is the invitation.`, idle.Hint)
	}
}

func TestActionLabelsLoseTheirChrome(t *testing.T) {
	cases := map[string]string{
		"Open Valheim Manager →":   "Open Valheim manager",
		"+ Create World":           "Create world",
		"Open Garry's Mod Manager": "Open Garry's Mod manager",
		"Access":                   "Access",
	}
	for in, want := range cases {
		if got := cleanActionLabel(in); got != want {
			t.Errorf("cleanActionLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAdminPanelCleansEveryActionGroup(t *testing.T) {
	section := BuildAdminPanel(adminSample())[0]
	if section.Actions.Manager.Label != "Open Valheim manager" {
		t.Errorf("manager label = %q", section.Actions.Manager.Label)
	}
	if section.Actions.Special[0].Label != "Create world" {
		t.Errorf("special label = %q", section.Actions.Special[0].Label)
	}
	if section.Actions.Manager.Href != "/valheim" {
		t.Error("cleaning a label must not disturb its href")
	}
}

func TestEveryGameRendersThroughTheSamePort(t *testing.T) {
	row := ServerRowUI{Name: "World", Address: "10.0.0.1:2456", Online: true, PlayersKnown: true, Players: 2, Uptime: "4h"}
	render := func(p PanelPortUI) string {
		var buf bytes.Buffer
		if err := panelPort(p).Render(context.Background(), &buf); err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		out = strings.ReplaceAll(out, p.Game, "GAME")
		return strings.ReplaceAll(out, p.Tint, "TINT")
	}

	valheim := render(PanelPortUI{Game: "valheim", Tint: tintClass("orange"), Row: row})
	minecraft := render(PanelPortUI{Game: "minecraft", Tint: tintClass("emerald"), Row: row})

	if valheim != minecraft {
		t.Error(`two games with identical data rendered different markup.

Every port goes through panelPort so the games cannot drift apart; a difference
here means something game-specific leaked into the shared component.`)
	}
}
