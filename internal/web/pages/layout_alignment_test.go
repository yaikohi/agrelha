package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func TestLayoutAndLayoutHearthShareIdenticalContainerDimensions(t *testing.T) {
	ctx := context.Background()

	var bufStandard bytes.Buffer
	if err := Layout("Test").Render(ctx, &bufStandard); err != nil {
		t.Fatalf("Layout render failed: %v", err)
	}

	var bufHearth bytes.Buffer
	if err := LayoutHearth("Test").Render(ctx, &bufHearth); err != nil {
		t.Fatalf("LayoutHearth render failed: %v", err)
	}

	wantClass := `class="mx-auto max-w-5xl overflow-x-hidden px-6 py-8"`
	if !strings.Contains(bufStandard.String(), wantClass) {
		t.Fatalf("Layout must use %s; at 2am an operator navigating between admin views and game manager experiences abrupt layout snapping and content reflow", wantClass)
	}
	if !strings.Contains(bufHearth.String(), wantClass) {
		t.Fatalf("LayoutHearth must use %s; at 2am an operator navigating between admin views and game manager experiences abrupt layout snapping and content reflow", wantClass)
	}
}

func TestAdminPagesSetCurrentNavTab(t *testing.T) {
	ctx := context.Background()

	var histBuf bytes.Buffer
	if err := History(nil).Render(ctx, &histBuf); err != nil {
		t.Fatalf("History render failed: %v", err)
	}
	if !strings.Contains(histBuf.String(), `href="/history" class="focus-accent inline-flex items-baseline gap-1.5 border-b-2 pb-1 transition-colors border-accent text-ink"`) {
		t.Fatalf("History page must render active navbar tab for history; at 2am an operator glancing at the navbar has no visual indication of their current screen")
	}

	var accessBuf bytes.Buffer
	if err := Access(AccessProps{}).Render(ctx, &accessBuf); err != nil {
		t.Fatalf("Access render failed: %v", err)
	}
	if !strings.Contains(accessBuf.String(), `href="/access" class="focus-accent inline-flex items-baseline gap-1.5 border-b-2 pb-1 transition-colors border-accent text-ink"`) {
		t.Fatalf("Access page must render active navbar tab for access; at 2am an operator has no visual indication of being on the access page")
	}

	var overviewBuf bytes.Buffer
	if err := Overview(nil, nil, 0, "", "").Render(ctx, &overviewBuf); err != nil {
		t.Fatalf("Overview render failed: %v", err)
	}
	if !strings.Contains(overviewBuf.String(), `href="/overview" class="focus-accent inline-flex items-baseline gap-1.5 border-b-2 pb-1 transition-colors border-accent text-ink"`) {
		t.Fatalf("Overview page must render active navbar tab for overview; at 2am an operator has no visual indication of being on the overview page")
	}

	var accountsBuf bytes.Buffer
	if err := Accounts(nil, nil, false, "", "", "").Render(ctx, &accountsBuf); err != nil {
		t.Fatalf("Accounts render failed: %v", err)
	}
	if !strings.Contains(accountsBuf.String(), `href="/accounts" class="focus-accent inline-flex items-baseline gap-1.5 border-b-2 pb-1 transition-colors border-accent text-ink"`) {
		t.Fatalf("Accounts page must render active navbar tab for accounts; at 2am an operator has no visual indication of being on the accounts page")
	}

	var reqBuf bytes.Buffer
	if err := Requests(nil, nil, "", "").Render(ctx, &reqBuf); err != nil {
		t.Fatalf("Requests render failed: %v", err)
	}
	if !strings.Contains(reqBuf.String(), `href="/requests" class="focus-accent inline-flex items-baseline gap-1.5 border-b-2 pb-1 transition-colors border-accent text-ink"`) {
		t.Fatalf("Requests page must render active navbar tab for requests; at 2am an operator has no visual indication of being on the requests page")
	}

	var consoleBuf bytes.Buffer
	if err := ValheimConsole(true, "", "").Render(ctx, &consoleBuf); err != nil {
		t.Fatalf("ValheimConsole render failed: %v", err)
	}
	if !strings.Contains(consoleBuf.String(), `href="/valheim" class="focus-accent inline-flex items-baseline gap-1.5 border-b-2 pb-1 transition-colors border-accent text-ink"`) {
		t.Fatalf("ValheimConsole page must render active navbar tab for valheim; at 2am an operator inspecting container logs loses context on which game console they are viewing")
	}
}

func TestManagerPortSignals(t *testing.T) {
	p := ManagerPortUI{
		Game:       "valheim",
		Number:     1,
		StateLabel: "Running",
		Presence:   "2 playing, up 4h",
	}

	if p.SignalPrefix() != "inst_valheim_1" {
		t.Fatalf("expected SignalPrefix 'inst_valheim_1', got %q; at 2am SSE metric stream bindings will misroute updates to an invalid signal key", p.SignalPrefix())
	}
	if !strings.Contains(p.LampSignals(), "$inst_valheim_1_online") || !strings.Contains(p.LampSignals(), "$inst_valheim_1_players") {
		t.Fatalf("expected LampSignals to bind to online and player signals, got %q; at 2am health lamp does not update over SSE without manual refresh", p.LampSignals())
	}
	if p.StateSignalExpr() != "$inst_valheim_1_state || 'Running'" {
		t.Fatalf("expected StateSignalExpr '$inst_valheim_1_state || 'Running'', got %q; at 2am state label fails to update on server start/stop", p.StateSignalExpr())
	}
	if p.PresenceShowExpr() != "$inst_valheim_1_online" {
		t.Fatalf("expected PresenceShowExpr '$inst_valheim_1_online', got %q; at 2am presence section fails to display when world comes online", p.PresenceShowExpr())
	}
	if !strings.Contains(p.PresenceExpr(), "$inst_valheim_1_players") || !strings.Contains(p.PresenceExpr(), "$inst_valheim_1_uptime") {
		t.Fatalf("expected PresenceExpr to compute player and uptime strings, got %q; at 2am player counts fail to stream to manager UI", p.PresenceExpr())
	}
}

func TestInstanceHeadSignals(t *testing.T) {
	h := InstanceHeadUI{
		Game:       "minecraft",
		Number:     2,
		StateLabel: "Stopped",
	}

	if h.SignalPrefix() != "inst_minecraft_2" {
		t.Fatalf("expected SignalPrefix 'inst_minecraft_2', got %q; at 2am instance detail stream bindings will target wrong signal namespace", h.SignalPrefix())
	}
	if !strings.Contains(h.LampSignals(), "$inst_minecraft_2_online") {
		t.Fatalf("expected LampSignals to bind to online signal, got %q; at 2am detail page lamp fails to reflect running state", h.LampSignals())
	}
	if h.StateSignalExpr() != "$inst_minecraft_2_state || 'Stopped'" {
		t.Fatalf("expected StateSignalExpr '$inst_minecraft_2_state || 'Stopped'', got %q; at 2am instance detail state label remains stale after start action", h.StateSignalExpr())
	}
}

func TestModResultInstalledExpr(t *testing.T) {
	r := ModResultUI{
		Slug:      "sodium",
		Installed: false,
	}

	expr := r.InstalledExpr("minecraft", 1)
	want := "($inst_minecraft_1_mods ? !!$inst_minecraft_1_mods['sodium'] : false)"
	if expr != want {
		t.Fatalf("expected InstalledExpr %q, got %q; at 2am mod install/remove buttons fail to toggle reactively after search operations", want, expr)
	}

	rInstalled := ModResultUI{
		Slug:      "fabric-api",
		Installed: true,
	}
	exprInstalled := rInstalled.InstalledExpr("minecraft", 1)
	wantInstalled := "($inst_minecraft_1_mods ? !!$inst_minecraft_1_mods['fabric-api'] : true)"
	if exprInstalled != wantInstalled {
		t.Fatalf("expected InstalledExpr %q, got %q; at 2am installed mods do not show as installed on search results", wantInstalled, exprInstalled)
	}
}

func TestWizardsDoNotUseBrowserAlert(t *testing.T) {
	ctx := context.Background()

	wizards := map[string]templ.Component{
		"valheim":   ValheimWizard(BudgetUI{}),
		"minecraft": MinecraftWizard([]string{"1.21.1"}, BudgetUI{}),
		"gmod":      GModWizard(BudgetUI{}),
	}

	for name, comp := range wizards {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := comp.Render(ctx, &buf); err != nil {
				t.Fatalf("failed to render %s wizard: %v", name, err)
			}
			out := buf.String()
			if strings.Contains(out, "alert(") {
				t.Fatalf("%s wizard contains browser alert(); at 2am an admin trapped in an unstyled modal dialog loses form context instead of receiving inline validation and Datastar toast feedback, got: %s", name, out)
			}
			if strings.Contains(out, "→") || strings.Contains(out, "←") {
				t.Fatalf("%s wizard contains trailing or leading arrow chrome on action buttons; at 2am buttons look like unpolished template chrome rather than crisp console controls, got: %s", name, out)
			}
		})
	}
}

func TestInstanceDetailServerTelemetryContainer(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	overview := instanceOverview(InstanceOverviewUI{
		Address: "192.168.20.224:2456",
		Facts: []InstanceFactUI{
			{Label: "World Name", Value: "Midgard"},
			{Label: "Port", Value: "2456"},
		},
	})
	if err := overview.Render(ctx, &buf); err != nil {
		t.Fatalf("instanceOverview render failed: %v", err)
	}
	out := buf.String()
	wantContainer := "rounded-control border border-edge bg-surface-raised/20 p-6"
	if !strings.Contains(out, wantContainer) {
		t.Fatalf("instanceOverview must wrap facts in %q; at 2am server telemetry unboxes into whitespace with no visual grouping, got: %s", wantContainer, out)
	}
	if !strings.Contains(out, "Server telemetry") {
		t.Fatalf("instanceOverview missing 'Server telemetry' heading; at 2am an operator inspecting telemetry cannot identify the metrics panel, got: %s", out)
	}
	wantLabel := `class="font-pixel text-xs text-ink-muted">World Name</dt>`
	if !strings.Contains(out, wantLabel) {
		t.Fatalf("instanceOverview must render fact labels with %q; at 2am fact labels drop below the typography floor into low-contrast 11px text, got: %s", wantLabel, out)
	}
}

