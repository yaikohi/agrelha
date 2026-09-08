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
	valheim := render(t, ValheimCard("192.168.20.224:2456", false))
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
	both := render(t, ValheimCard("192.168.20.224:2456", true)) +
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
	valheim := render(t, ValheimCard("192.168.20.224:2456", false))
	minecraft := render(t, MinecraftCard(sampleMinecraft(), false))

	if !strings.Contains(valheim, ".r2z") || !strings.Contains(minecraft, ".mrpack") {
		t.Fatal("each card must still show its own modpack format badge")
	}
	if strings.Contains(valheim, ".mrpack") || strings.Contains(minecraft, ".r2z") {
		t.Fatal("format badges leaked across cards")
	}
}

// Valheim is signal-driven, Minecraft is server-rendered. Both must work.
func TestStatsRenderFromEitherSource(t *testing.T) {
	valheim := render(t, ValheimCard("192.168.20.224:2456", false))
	if !strings.Contains(valheim, `data-text="$players"`) || !strings.Contains(valheim, `data-text="$uptime"`) {
		t.Fatal("Valheim row must bind stats to live signals")
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
}

func TestOfflineCardShowsEmptyStateAndOfflinePill(t *testing.T) {
	out := render(t, MinecraftCard(MinecraftSummaryUI{MaxInstances: 4, MaxRunning: 2}, false))
	if !strings.Contains(out, "All Minecraft worlds are currently offline.") {
		t.Fatal("missing empty state")
	}
	if !strings.Contains(out, "Offline") {
		t.Fatal("header pill should read Offline when nothing runs")
	}
}

func TestServersOnlineLabelWording(t *testing.T) {
	for n, want := range map[int]string{0: "Offline", 1: "1 online", 2: "2 online"} {
		if got := ServersOnlineLabel(n); got != want {
			t.Fatalf("ServersOnlineLabel(%d) = %q, want %q", n, got, want)
		}
	}
}
