package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func panelFixture() PanelUI {
	return BuildPanel([]DashboardGameUI{
		{Card: GameCardUI{Title: "Valheim Worlds", Accent: "orange", AccessNote: "ask the host", Rows: []ServerRowUI{
			{Name: "Ducktopia", Address: "10.0.0.1:2456", Online: true, PlayersKnown: true, Players: 2},
		}}},
		{Card: GameCardUI{Title: "Minecraft Worlds", Accent: "emerald", EmptyHint: "ask"}},
		{Card: GameCardUI{Title: "Garry's Mod Worlds", Accent: "sky", Rows: []ServerRowUI{
			{Name: "Sandbox", Address: "10.0.0.2:27015", Online: true},
			{Name: "TTT", Address: "10.0.0.3:27015"},
		}}},
	})
}

func TestBuildPanelGroupsWorldsUnderTheirGame(t *testing.T) {
	p := panelFixture()

	if len(p.Games) != 3 {
		t.Fatalf("games = %d, want 3; a game with no worlds still belongs on the page", len(p.Games))
	}
	want := []struct {
		label  string
		worlds int
	}{{"valheim", 1}, {"minecraft", 0}, {"garry's mod", 2}}
	for i, w := range want {
		if p.Games[i].Label != w.label || len(p.Games[i].Worlds) != w.worlds {
			t.Errorf("game %d = %q with %d worlds, want %q with %d",
				i, p.Games[i].Label, len(p.Games[i].Worlds), w.label, w.worlds)
		}
	}
	if p.Games[1].Hint != "ask" {
		t.Errorf("the empty game lost its hint: %q", p.Games[1].Hint)
	}
	if p.Games[0].Worlds[0].AccessNote != "ask the host" {
		t.Errorf("the game's access note did not reach its world")
	}
	if p.Games[2].Tint != tintClass("sky") {
		t.Errorf("tint did not follow the card accent")
	}
}

func TestBuildPanelNumbersEveryWorldOnThePageOnce(t *testing.T) {
	p := panelFixture()

	var seen []int
	for _, g := range p.Games {
		for _, w := range g.Worlds {
			seen = append(seen, w.Index)
		}
	}
	for i, got := range seen {
		if got != i {
			// The index names the copy-button signals, and those share one
			// scope across games: a repeat makes two buttons move together.
			t.Errorf("world %d has Index %d; copy signals would collide", i, got)
		}
	}
}

func TestBuildPanelTallyCountsRunningWorldsAndPlayers(t *testing.T) {
	p := panelFixture()

	if p.Tally.Worlds != 3 {
		t.Errorf("worlds = %d, want 3", p.Tally.Worlds)
	}
	if p.Tally.Lit != 2 {
		t.Errorf("lit = %d, want 2; lit means running, with or without players", p.Tally.Lit)
	}
	if p.Tally.Playing != 2 {
		t.Errorf("playing = %d, want 2; a world with no known count adds nobody", p.Tally.Playing)
	}

	empty := BuildPanel(nil)
	if empty.Tally != (PanelTallyUI{}) || len(empty.Games) != 0 {
		t.Errorf("no games should tally to nothing, got %+v", empty)
	}
}

func TestPanelSlugSurvivesPunctuation(t *testing.T) {
	cases := map[string]string{
		"valheim":     "valheim",
		"garry's mod": "garry-s-mod",
		"Minecraft":   "minecraft",
		"  spaced  ":  "spaced",
	}
	for in, want := range cases {
		if got := panelSlug(in); got != want {
			t.Errorf("panelSlug(%q) = %q, want %q", in, got, want)
		}
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
	if p.LampClass() != "lamp lamp-out" {
		t.Errorf("expected lamp lamp-out for offline row, got %q", p.LampClass())
	}
	pOnline := PanelPortUI{Row: ServerRowUI{OnlineSignal: "online", PlayersSignal: "players", Online: true}}
	if pOnline.LampClass() != "lamp lamp-dim" {
		t.Errorf("expected lamp lamp-dim for online row without active players, got %q", pOnline.LampClass())
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

func TestSignalsInit(t *testing.T) {
	withAll := PanelPortUI{Row: ServerRowUI{
		OnlineSignal:  "inst_minecraft_2_online",
		Online:        true,
		PlayersSignal: "inst_minecraft_2_players",
		Players:       3,
		UptimeSignal:  "inst_minecraft_2_uptime",
		Uptime:        "2h",
	}}
	got := withAll.SignalsInit()
	want := "{inst_minecraft_2_online:true,inst_minecraft_2_players:3,inst_minecraft_2_uptime:'2h'}"
	if got != want {
		t.Errorf("SignalsInit() = %q, want %q", got, want)
	}

	noSignals := PanelPortUI{Row: ServerRowUI{}}
	if got := noSignals.SignalsInit(); got != "" {
		t.Errorf("SignalsInit() for unsignaled row = %q, want empty string", got)
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
	p := PanelPortUI{Row: ServerRowUI{OnlineSignal: "online", PlayersSignal: "players", UptimeSignal: "uptime", PlayersKnown: true}}
	expr := p.PresenceExpr()
	for _, want := range []string{"$online", "$players", "$uptime", "playing", "nobody on", "Not running"} {
		if !strings.Contains(expr, want) {
			t.Errorf("PresenceExpr() missing %q: %s", want, expr)
		}
	}
	if !strings.Contains(expr, "'—'") {
		t.Error("an unresolved uptime signal would be printed verbatim as a dash")
	}

	bare := PanelPortUI{Row: ServerRowUI{OnlineSignal: "online", PlayersKnown: true}}
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

func TestPanelPortConnectionPlateAndTitleProminence(t *testing.T) {
	port := PanelPortUI{
		Index: 0,
		Game:  "valheim",
		Tint:  "text-accent",
		Row: ServerRowUI{
			Name:    "Ducktopia",
			Address: "10.0.0.1:2456",
			Online:  true,
		},
	}
	var buf bytes.Buffer
	if err := panelPort(port).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "text-3xl") {
		t.Fatalf("panel card does not render world title with text-3xl prominence; at 2am players confuse the large raw IP address with the world title, got: %s", out)
	}
	if !strings.Contains(out, "bg-surface-raised/40") || !strings.Contains(out, "border-edge") {
		t.Fatalf("panel card does not render the connection address within a distinct connection plate; at 2am an uncontained floating address creates visual disarray, got: %s", out)
	}
	if !strings.Contains(out, ">copy<") && !strings.Contains(out, "data-text") {
		t.Fatalf("panel card is missing copy chip affordance; at 2am an operator or player cannot immediately discover one-click copy feedback, got: %s", out)
	}
}

func TestOnlyTheFrontPageTakesTheWideShell(t *testing.T) {
	games := []DashboardGameUI{
		{Card: GameCardUI{Title: "Valheim Worlds", Accent: "orange", Rows: []ServerRowUI{
			{Name: "Ducktopia", Address: "10.0.0.1:2456", Online: true},
		}}},
		{Card: GameCardUI{Title: "Minecraft Worlds", Accent: "emerald", EmptyHint: "ask the host"}},
	}

	var front bytes.Buffer
	if err := Panel(games, false, "", "").Render(context.Background(), &front); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(front.String(), "max-w-[1680px]") {
		t.Error("the front page is still capped at the narrow measure")
	}
	// The worlds and the game with none both survived the regrouping.
	for _, want := range []string{"Ducktopia", "10.0.0.1:2456", "minecraft", "ask the host"} {
		if !strings.Contains(front.String(), want) {
			t.Errorf("the page lost %q", want)
		}
	}

	var hub bytes.Buffer
	if err := AdminHub("", games, true, "", "").Render(context.Background(), &hub); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hub.String(), "max-w-[1680px]") {
		t.Error("the admin hub widened with it; its layout has not been redesigned yet")
	}
	if !strings.Contains(hub.String(), "max-w-5xl") {
		t.Error("the admin hub lost its measure entirely")
	}
}

func TestTallyHeadlineReadsAsASentence(t *testing.T) {
	cases := []struct {
		name        string
		tally       PanelTallyUI
		lit, asleep string
	}{
		{"nothing created yet", PanelTallyUI{}, "No worlds yet.", ""},
		{"the only world is off", PanelTallyUI{Worlds: 1}, "The world is sleeping.", ""},
		{"all of them off", PanelTallyUI{Worlds: 3}, "Nothing is running right now.", ""},
		{"one on, none off", PanelTallyUI{Worlds: 1, Lit: 1}, "One world is lit.", ""},
		{"one on, one off", PanelTallyUI{Worlds: 2, Lit: 1}, "One world is lit.", "One is sleeping."},
		{"two on, one off", PanelTallyUI{Worlds: 3, Lit: 2}, "Two worlds are lit.", "One is sleeping."},
		{"two on, three off", PanelTallyUI{Worlds: 5, Lit: 2}, "Two worlds are lit.", "Three are sleeping."},
		{"all on", PanelTallyUI{Worlds: 4, Lit: 4}, "Four worlds are lit.", ""},
		{"past the words", PanelTallyUI{Worlds: 14, Lit: 12}, "12 worlds are lit.", "Two are sleeping."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lit, asleep := c.tally.Headline()
			if lit != c.lit || asleep != c.asleep {
				t.Errorf("Headline() = %q + %q, want %q + %q", lit, asleep, c.lit, c.asleep)
			}
		})
	}
}

// A count nobody could read is not a count of zero. Reporting "nobody on" for a
// world the panel cannot ask is how a server with two people playing on it read
// as empty, with nothing on the page saying the number was never obtained.
func TestAnUnreadableCountDoesNotReadAsAnEmptyServer(t *testing.T) {
	row := ServerRowUI{
		Online: true, Uptime: "2d 22h",
		PlayersKnown: false,
		OnlineSignal: "online", PlayersSignal: "players", UptimeSignal: "uptime",
	}
	p := PanelPortUI{Row: row}

	got := p.Presence()
	if strings.Contains(got, "nobody on") {
		t.Errorf("Presence() = %q, which claims an empty server the panel never managed to count", got)
	}
	if !strings.Contains(got, "unknown") {
		t.Errorf("Presence() = %q, want it to say the count is unknown", got)
	}
	if !strings.Contains(got, "2d 22h") {
		t.Errorf("Presence() = %q, want the uptime kept", got)
	}
	if expr := p.PresenceExpr(); expr != "" {
		t.Errorf("PresenceExpr() = %q: binding a player signal there would overwrite the honest text with 'nobody on'", expr)
	}
}
