package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func render(t *testing.T, g GameCardUI) string {
	t.Helper()
	var buf bytes.Buffer
	if err := gameCard(g).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func sampleMinecraft() MinecraftSummaryUI {
	return MinecraftSummaryUI{
		TotalInstances: 2, MaxInstances: 4,
		RunningCount: 1, MaxRunning: 2,
		UsedGiB: 8, TotalBudgetGiB: 24,
		ActiveInstances: []InstanceUI{{
			Number: 1, Name: "ayyy", LBIP: "192.168.20.225",
			MCVersion: "1.21.1", Loader: "neoforge",
			Players: 3, PlayersKnown: true, Uptime: "4h 2m",
		}},
	}
}

// The two cards must expose the same functionality through the same markup.
// This is the guard against them drifting apart again.
func TestBothCardsShareTheSameSections(t *testing.T) {
	valheim := render(t, ValheimCard("192.168.20.224:2456", "game-01", false))
	minecraft := render(t, MinecraftCard(sampleMinecraft(), false))

	for _, section := range []string{
		"Online Players", // stats live in the row on both
		"Uptime",
		"Download Modpack", // one label on both; format is a badge
		">Copy<",           // address + copy on both
		"Import into",      // launcher instructions on both
		"Server specifics", // the extras strip
	} {
		if !strings.Contains(valheim, section) {
			t.Errorf("Valheim card missing shared section %q", section)
		}
		if !strings.Contains(minecraft, section) {
			t.Errorf("Minecraft card missing shared section %q", section)
		}
	}
}

// Regressions we specifically fixed: the labels and status wording used to
// differ between the cards for identical functionality.
func TestDivergentLabelsAreGone(t *testing.T) {
	both := render(t, ValheimCard("192.168.20.224:2456", "game-01", true)) +
		render(t, MinecraftCard(sampleMinecraft(), true))

	for _, gone := range []string{
		"Download Pack (.mrpack)", // Minecraft's old bespoke button label
		"Client Modpack",          // Valheim's old bespoke panel heading
		"Active</span>",           // "1 Active" pill; both now say "N online"
	} {
		if strings.Contains(both, gone) {
			t.Errorf("divergent label %q is still rendered", gone)
		}
	}
}

func TestFormatBadgesDifferButButtonDoesNot(t *testing.T) {
	valheim := render(t, ValheimCard("192.168.20.224:2456", "game-01", false))
	minecraft := render(t, MinecraftCard(sampleMinecraft(), false))

	if !strings.Contains(valheim, ".r2z") || !strings.Contains(minecraft, ".mrpack") {
		t.Fatal("each card must still show its own modpack format badge")
	}
	if strings.Contains(valheim, ".mrpack") || strings.Contains(minecraft, ".r2z") {
		t.Fatal("format badges leaked across cards")
	}
}

func sampleValheim() ValheimSummaryUI {
	return ValheimSummaryUI{
		TotalInstances: 2, MaxInstances: 4,
		RunningCount:   1, MaxRunning: 2,
		UsedGiB:        6, TotalBudgetGiB: 16,
		ActiveInstances: []InstanceUI{{
			Number: 1, Name: "lareira-V2", LBIP: "192.168.20.224",
			Source:       "modpack",
			Players:      2, PlayersKnown: true, Uptime: "1h 30m",
		}},
	}
}

// Both Valheim and Minecraft cards are multi-instance and render stats from active instances.
func TestStatsRenderFromActiveInstances(t *testing.T) {
	valheim := render(t, ValheimCard("192.168.20.224:2456", "game-01", false, sampleValheim()))
	if !strings.Contains(valheim, ">2<") || !strings.Contains(valheim, "1h 30m") {
		t.Fatal("Valheim row must print its instance stats")
	}

	minecraft := render(t, MinecraftCard(sampleMinecraft(), false))
	if !strings.Contains(minecraft, ">3<") || !strings.Contains(minecraft, "4h 2m") {
		t.Fatal("Minecraft row must print its server-rendered stats")
	}
}

func TestUnknownStatsDegradeToDash(t *testing.T) {
	mc := sampleMinecraft()
	mc.ActiveInstances[0].PlayersKnown = false
	mc.ActiveInstances[0].Uptime = ""
	out := render(t, MinecraftCard(mc, false))
	if strings.Count(out, ">—<") < 2 {
		t.Fatal("unreachable stats must render as — rather than a misleading 0")
	}

	vh := sampleValheim()
	vh.ActiveInstances[0].PlayersKnown = false
	vh.ActiveInstances[0].Uptime = ""
	outVh := render(t, ValheimCard("192.168.20.224:2456", "game-01", false, vh))
	if strings.Count(outVh, ">—<") < 2 {
		t.Fatal("unreachable Valheim stats must render as — rather than a misleading 0")
	}
}

func TestOfflineCardShowsEmptyStateAndOfflinePill(t *testing.T) {
	outMC := render(t, MinecraftCard(MinecraftSummaryUI{MaxInstances: 4, MaxRunning: 2}, false))
	if !strings.Contains(outMC, "All Minecraft worlds are currently offline.") {
		t.Fatal("missing MC empty state")
	}
	if !strings.Contains(outMC, "Offline") {
		t.Fatal("header pill should read Offline when nothing runs")
	}

	outVH := render(t, ValheimCard("192.168.20.224:2456", "game-01", false, ValheimSummaryUI{MaxInstances: 4, MaxRunning: 2}))
	if !strings.Contains(outVH, "All Valheim worlds are currently offline.") {
		t.Fatal("missing Valheim empty state")
	}
	if !strings.Contains(outVH, "Offline") {
		t.Fatal("Valheim header pill should read Offline when nothing runs")
	}
}

func TestServersOnlineLabelWording(t *testing.T) {
	for n, want := range map[int]string{0: "Offline", 1: "1 online", 2: "2 online"} {
		if got := ServersOnlineLabel(n); got != want {
			t.Fatalf("ServersOnlineLabel(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestBothCardsShowOnlinePillWhenRunning(t *testing.T) {
	valheim := render(t, ValheimCard("192.168.20.224:2456", "game-01", false, sampleValheim()))
	if !strings.Contains(valheim, "1 online") {
		t.Fatal("Valheim card header pill should read 1 online when an active instance runs")
	}

	minecraft := render(t, MinecraftCard(sampleMinecraft(), false))
	if !strings.Contains(minecraft, "1 online") {
		t.Fatal("Minecraft card header pill should read 1 online when an active instance runs")
	}
}

func renderActions(t *testing.T, a CardActionsUI) string {
	t.Helper()
	var buf bytes.Buffer
	if err := cardActions(a).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// Both games have the same action set (both have configs and access pages, and
// both have start/stop/restart endpoints), so the footers must offer the same
// controls in the same order.
func TestBothFootersOfferTheSameActions(t *testing.T) {
	valheim := renderActions(t, ValheimActions())
	minecraft := renderActions(t, MinecraftActions(sampleMinecraft()))

	for _, label := range []string{"Restart", "Stop", "Start", "Configs", "Access", "Manager"} {
		if !strings.Contains(valheim, label) {
			t.Errorf("Valheim footer missing %q", label)
		}
		if !strings.Contains(minecraft, label) {
			t.Errorf("Minecraft footer missing %q", label)
		}
	}
}

// Game-specific controls belong in the marked Special slot, not mixed in.
func TestGameSpecificActionsAreMarked(t *testing.T) {
	valheim := renderActions(t, ValheimActions())
	if !strings.Contains(valheim, "Update") {
		t.Fatal("Valheim should still offer Update")
	}
	if !strings.Contains(valheim, "border-dashed") {
		t.Fatal("Update is Valheim-only and must use the marked Special styling")
	}

	idle := renderActions(t, MinecraftActions(MinecraftSummaryUI{MaxInstances: 4}))
	if !strings.Contains(idle, "Create World") || !strings.Contains(idle, "border-dashed") {
		t.Fatal("Create World is Minecraft-only and must use the marked Special styling")
	}
	if strings.Contains(idle, "Restart") {
		t.Fatal("with no instance running there is nothing to restart")
	}
}

// Same role => same styling on both cards, so colour always means one thing.
func TestActionRolesStyleIdentically(t *testing.T) {
	valheim := renderActions(t, ValheimActions())
	minecraft := renderActions(t, MinecraftActions(sampleMinecraft()))

	for _, cls := range []string{
		actionStyle("danger"), // Stop
		actionStyle("go"),     // Start
		actionStyle("link"),   // Configs / Access
	} {
		if !strings.Contains(valheim, cls) || !strings.Contains(minecraft, cls) {
			t.Fatalf("role styling %q is not shared by both footers", cls)
		}
	}
}

func renderNav(t *testing.T, g NavGroupUI) string {
	t.Helper()
	var buf bytes.Buffer
	if err := navGroup(g).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// The game entries lead directly to each game's world instances dashboard.
func TestNavGameEntriesLeadToServerDashboards(t *testing.T) {
	if got := ValheimNav().Href; got != "/valheim" {
		t.Fatalf("Valheim nav entry = %q, want /valheim", got)
	}
	if got := MinecraftNav().Href; got != "/minecraft" {
		t.Fatalf("Minecraft nav entry = %q, want /minecraft", got)
	}
	if got := ValheimNav().Label; got != "Valheim" {
		t.Fatalf("Valheim label = %q, want Valheim", got)
	}
	if got := MinecraftNav().Label; got != "Minecraft" {
		t.Fatalf("Minecraft label = %q, want Minecraft", got)
	}
}
