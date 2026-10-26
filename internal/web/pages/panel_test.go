package pages

import (
	"strings"
	"testing"
)

func TestBuildPanelFlattensRowsAndKeepsIdleGamesSeparate(t *testing.T) {
	p := BuildPanel([]DashboardGameUI{
		{Card: GameCardUI{Title: "Valheim Worlds", Accent: "orange", AccessNote: "ask the host", Rows: []ServerRowUI{
			{Name: "Ducktopia", Address: "10.0.0.1:2456"},
		}}},
		{Card: GameCardUI{Title: "Minecraft Worlds", Accent: "emerald", EmptyHint: "ask"}},
		{Card: GameCardUI{Title: "Garry's Mod Worlds", Accent: "sky", Rows: []ServerRowUI{
			{Name: "Sandbox", Address: "10.0.0.2:27015"},
			{Name: "TTT", Address: "10.0.0.3:27015"},
		}}},
	})

	if len(p.Ports) != 3 {
		t.Fatalf("ports = %d, want 3", len(p.Ports))
	}
	if len(p.Idle) != 1 {
		t.Fatalf("idle = %d, want 1", len(p.Idle))
	}

	for i, port := range p.Ports {
		if port.Index != i {
			t.Errorf("port %d has Index %d; copy signals would collide", i, port.Index)
		}
	}

	if p.Ports[0].Game != "valheim" {
		t.Errorf("game label = %q, want %q", p.Ports[0].Game, "valheim")
	}
	if p.Ports[0].AccessNote != "ask the host" {
		t.Errorf("the game's access note did not reach its port")
	}
	if p.Idle[0].Game != "minecraft" {
		t.Errorf("idle label = %q, want %q", p.Idle[0].Game, "minecraft")
	}
	if p.Ports[2].Tint != tintClass("sky") {
		t.Errorf("tint did not follow the card accent")
	}
}

func TestLampReportsPresenceNotJustReachability(t *testing.T) {
	cases := []struct {
		name string
		row  ServerRowUI
		want string
	}{
		{"players on", ServerRowUI{Online: true, PlayersKnown: true, Players: 3}, "lamp lamp-lit"},
		{"up but empty", ServerRowUI{Online: true, PlayersKnown: true}, "lamp lamp-dim"},
		{"count unknown", ServerRowUI{Online: true}, "lamp lamp-dim"},
		{"not running", ServerRowUI{}, "lamp lamp-out"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := (PanelPortUI{Row: c.row}).LampClass(); got != c.want {
				t.Errorf("LampClass() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestLampBindsToSignalsWhenTheyExist(t *testing.T) {
	p := PanelPortUI{Row: ServerRowUI{OnlineSignal: "online", PlayersSignal: "players"}}
	if p.LampClass() != "lamp" {
		t.Errorf("a signal-driven lamp must not bake in a state class, got %q", p.LampClass())
	}
	expr := p.LampSignals()
	for _, want := range []string{"lamp-lit", "lamp-dim", "lamp-out", "$online", "$players"} {
		if !strings.Contains(expr, want) {
			t.Errorf("LampSignals() missing %q: %s", want, expr)
		}
	}

	noPlayers := PanelPortUI{Row: ServerRowUI{OnlineSignal: "online"}}
	if strings.Contains(noPlayers.LampSignals(), "lamp-lit") {
		t.Error("without a player signal the lamp cannot claim anyone is playing")
	}
	if (PanelPortUI{Row: ServerRowUI{}}).LampSignals() != "" {
		t.Error("a server-rendered row must not emit a data-class expression")
	}
}

func TestPresenceReadsAsASentence(t *testing.T) {
	cases := []struct {
		name string
		row  ServerRowUI
		want string
	}{
		{"with players and uptime", ServerRowUI{Online: true, PlayersKnown: true, Players: 2, Uptime: "4h"}, "2 playing, up 4h"},
		{"empty with uptime", ServerRowUI{Online: true, PlayersKnown: true, Uptime: "4h"}, "nobody on, up 4h"},
		{"uptime unknown", ServerRowUI{Online: true, PlayersKnown: true, Uptime: "—"}, "nobody on"},
		{"offline", ServerRowUI{Uptime: "4h"}, "Not running"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := (PanelPortUI{Row: c.row}).Presence(); got != c.want {
				t.Errorf("Presence() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestCopyIsPerPortAndEscaped(t *testing.T) {
	p := PanelPortUI{Index: 2, Row: ServerRowUI{Address: "10.0.0.1:2456"}}
	click := p.CopyClick()
	if !strings.Contains(click, "$copied_2 = true") {
		t.Errorf("copy click does not target its own port: %s", click)
	}
	if !strings.Contains(p.CopyInit(), "copied_2:false") {
		t.Errorf("copy signal is not initialised: %s", p.CopyInit())
	}
	if strings.Contains(click, "navigator.clipboard") {
		t.Error("the copy helper must be used so plain-http LAN access still copies")
	}

	hostile := PanelPortUI{Row: ServerRowUI{Address: "a'; alert(1); x='"}}
	if strings.Contains(hostile.CopyClick(), "'; alert(1); x='") {
		t.Error("address is interpolated into a script without escaping")
	}
}

func TestLivePresenceExpressionCoversEveryState(t *testing.T) {
	p := PanelPortUI{Row: ServerRowUI{OnlineSignal: "online", PlayersSignal: "players", UptimeSignal: "uptime"}}
	expr := p.PresenceExpr()
	for _, want := range []string{"$online", "$players", "$uptime", "playing", "nobody on", "Not running"} {
		if !strings.Contains(expr, want) {
			t.Errorf("PresenceExpr() missing %q: %s", want, expr)
		}
	}
	if !strings.Contains(expr, "'—'") {
		t.Error("an unresolved uptime signal would be printed verbatim as a dash")
	}

	bare := PanelPortUI{Row: ServerRowUI{OnlineSignal: "online"}}
	if strings.Contains(bare.PresenceExpr(), "playing") {
		t.Error("without a player signal the line cannot claim a player count")
	}
	if (PanelPortUI{Row: ServerRowUI{}}).PresenceExpr() != "" {
		t.Error("a server-rendered row must not emit a data-text expression")
	}

	label := (PanelPortUI{Index: 1}).CopyLabelExpr()
	if !strings.Contains(label, "$copied_1") || !strings.Contains(label, "copied") {
		t.Errorf("copy label does not reflect its own port: %s", label)
	}
}

func TestImportHintOnlyAppearsWithADownload(t *testing.T) {
	with := PanelPortUI{Row: ServerRowUI{
		DownloadURL: "/d.r2z",
		Launchers:   []string{"r2modman", "Thunderstore Mod Manager"},
		ImportSteps: "Import → From file",
	}}
	want := "Import into r2modman or Thunderstore Mod Manager (Import → From file)"
	if got := with.ImportHint(); got != want {
		t.Errorf("ImportHint() = %q, want %q", got, want)
	}

	noDownload := PanelPortUI{Row: ServerRowUI{Launchers: []string{"r2modman"}}}
	if noDownload.ImportHint() != "" {
		t.Error("an import hint without a download tells people to import nothing")
	}
}
