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
			MCVersion: "1.21.1", Loader: "neoforge", HasMods: true,
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
		RunningCount: 1, MaxRunning: 2,
		UsedGiB: 6, TotalBudgetGiB: 16,
		ActiveInstances: []InstanceUI{{
			Number: 1, Name: "lareira-V2", LBIP: "192.168.20.224",
			Source: "modpack", HasMods: true,
			Players: 2, PlayersKnown: true, Uptime: "1h 30m",
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

func TestOfflineCardShowsEmptyStateWithoutAPill(t *testing.T) {
	outMC := render(t, MinecraftCard(MinecraftSummaryUI{MaxInstances: 4, MaxRunning: 2}, false))
	if !strings.Contains(outMC, "All Minecraft worlds are currently offline.") {
		t.Fatal("missing MC empty state")
	}

	outVH := render(t, ValheimCard("192.168.20.224:2456", "game-01", false, ValheimSummaryUI{MaxInstances: 4, MaxRunning: 2}))
	if !strings.Contains(outVH, "All Valheim worlds are currently offline.") {
		t.Fatal("missing Valheim empty state")
	}
}

func TestServersOnlineLabelWording(t *testing.T) {
	for n, want := range map[int]string{0: "Offline", 1: "1 online", 2: "2 online"} {
		if got := ServersOnlineLabel(n); got != want {
			t.Fatalf("ServersOnlineLabel(%d) = %q, want %q", n, got, want)
		}
	}
}

// The header counter is gone: the rows themselves say which Worlds are online,
// and a count only earns its place on an overview spanning more than one game.
func TestNeitherCardShowsAHeaderCounterOrAccessPill(t *testing.T) {
	both := render(t, ValheimCard("192.168.20.224:2456", "game-01", false, sampleValheim())) +
		render(t, MinecraftCard(sampleMinecraft(), false))

	// The AccessNote sentence stays - it tells a player how to get in. It is the
	// header tag that goes.
	for _, gone := range []string{"1 online", "2 online", "🔒 Password", "🛡️ Whitelist"} {
		if strings.Contains(both, gone) {
			t.Errorf("%q should no longer render on the hub card", gone)
		}
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

// The footers hold only game-scoped controls. Lifecycle and Configs used to act
// on ActiveInstances[0] - an unnamed World - so they moved to the Manager, where
// the target is named.
func TestBothFootersOfferTheSameActions(t *testing.T) {
	valheim := renderActions(t, ValheimActions())
	minecraft := renderActions(t, MinecraftActions(sampleMinecraft()))

	for _, label := range []string{"Access", "Manager"} {
		if !strings.Contains(valheim, label) {
			t.Errorf("Valheim footer missing %q", label)
		}
		if !strings.Contains(minecraft, label) {
			t.Errorf("Minecraft footer missing %q", label)
		}
	}
	for _, gone := range []string{"Restart", "Stop", "Start", "Configs", "Update"} {
		if strings.Contains(valheim, gone) {
			t.Errorf("Valheim footer still offers %q, which has no unambiguous target", gone)
		}
		if strings.Contains(minecraft, gone) {
			t.Errorf("Minecraft footer still offers %q, which has no unambiguous target", gone)
		}
	}
}

// Nothing game-specific remains in the footer: Update pointed at the legacy
// global endpoint, which no longer has a deployment behind it.
func TestNoGameSpecificFooterActionsRemain(t *testing.T) {
	// A populated card offers no lifecycle or per-instance controls...
	if a := ValheimActions(sampleValheim()); len(a.Special) != 0 || len(a.Lifecycle) != 0 {
		t.Errorf("valheim footer should hold only links when worlds exist: %+v", a)
	}
	if a := MinecraftActions(sampleMinecraft()); len(a.Special) != 0 || len(a.Lifecycle) != 0 {
		t.Errorf("minecraft footer should hold only links when worlds exist: %+v", a)
	}

	// ...but an empty card still offers the one unambiguous action there is.
	if a := ValheimActions(ValheimSummaryUI{}); len(a.Special) != 1 {
		t.Error("an empty Valheim card must still offer + Create World")
	}
	if a := MinecraftActions(MinecraftSummaryUI{}); len(a.Special) != 1 {
		t.Error("an empty Minecraft card must still offer + Create World")
	}
}

// Same role => same styling on both cards, so colour always means one thing.
func TestActionRolesStyleIdentically(t *testing.T) {
	valheim := renderActions(t, ValheimActions())
	minecraft := renderActions(t, MinecraftActions(sampleMinecraft()))

	// Only the link role survives in the footer; danger/go moved to the Manager
	// with the lifecycle controls they styled.
	for _, cls := range []string{
		actionStyle("link"), // Access
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
