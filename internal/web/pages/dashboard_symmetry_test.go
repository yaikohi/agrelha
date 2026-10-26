package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

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
