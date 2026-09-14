package pages

import (
	"strings"
	"testing"
	"time"

	"agrelha/internal/domain"
)

func TestConnectAddressShowsPendingHonestly(t *testing.T) {
	if got := ConnectAddress("192.168.20.241", 25565); got != "192.168.20.241:25565" {
		t.Errorf("got %q", got)
	}
	if got := ConnectAddress("", 25565); got == ":25565" || got == "25565" {
		t.Errorf("an unallocated LoadBalancer must not render as an address: %q", got)
	}
	if got := ConnectAddress("   ", 2456); got != "awaiting address" {
		t.Errorf("blank address = %q, want a pending message", got)
	}
}

func TestHelpersBasic(t *testing.T) {
	// ModKey
	if got := ModKey("author/mod/1.0"); got != "author/mod" {
		t.Errorf("ModKey = %s, want author/mod", got)
	}
	if got := ModKey("plain"); got != "plain" {
		t.Errorf("ModKey = %s, want plain", got)
	}

	// UpdateToken
	if got := UpdateToken("foo.bar-baz/1"); got != "foo_bar_baz_1" {
		t.Errorf("UpdateToken = %s", got)
	}

	// Contains
	if !Contains([]string{"a", "b"}, "a") || Contains([]string{"a", "b"}, "c") {
		t.Errorf("Contains helper failure")
	}

	// EscapeJS
	if got := EscapeJS(`a'b"c\d`); got != `a\'b\"c\\d` {
		t.Errorf("EscapeJS = %s", got)
	}

	// ModRefNames & ModUpdateViews
	ref, _ := domain.ParseModRef("denikson/BepInEx/5.4.0", domain.GameValheim)
	refs := []domain.ModRef{ref}
	names := ModRefNames(refs)
	if len(names) != 1 || !strings.Contains(names[0], "denikson") {
		t.Errorf("ModRefNames = %v", names)
	}

	ups := []domain.ModUpdate{
		{Ref: refs[0], Current: "5.4.0", Latest: "5.5.0"},
	}
	views := ModUpdateViews(ups)
	if len(views) != 1 || views[0].Current != "5.4.0" || views[0].Latest != "5.5.0" {
		t.Errorf("ModUpdateViews = %v", views)
	}
}

func TestHistoryLabelAndBadge(t *testing.T) {
	kinds := []string{
		"restart", "stop", "start", "mod-install", "mod-remove", "admin-grant", "admin-revoke",
		"mc-mod-install", "mc-mod-remove", "mc-modpack-switch", "mc-version-set", "mc-op-grant",
		"mc-op-revoke", "mc-whitelist-add", "mc-whitelist-remove", "mc-instance-create",
		"mc-instance-start", "mc-instance-stop", "mc-instance-delete", "join", "leave", "backup",
		"update", "valheim-mod-update", "valheim-mod-revert", "mc-mod-update", "crash", "unknown-kind",
	}
	for _, k := range kinds {
		lbl := HistoryLabel(k)
		if lbl == "" {
			t.Errorf("expected non-empty label for %s", k)
		}
		badge := HistoryBadge("action", k)
		if badge == "" {
			t.Errorf("expected non-empty badge for %s", k)
		}
	}
	if b := HistoryBadge("other", "unknown"); !strings.Contains(b, "zinc") {
		t.Errorf("expected default badge for unknown, got: %s", b)
	}
}

func TestServerRowAndCardUI(t *testing.T) {
	row := ServerRowUI{
		Name:         "Survival",
		Address:      "1.2.3.4:25565",
		Online:       true,
		Players:      3,
		PlayersKnown: true,
		Uptime:       "2h",
	}
	if row.PlayersText() != "3" {
		t.Errorf("PlayersText = %s, want 3", row.PlayersText())
	}
	if row.UptimeText() != "2h" {
		t.Errorf("UptimeText = %s, want 2h", row.UptimeText())
	}
	if !strings.Contains(row.CopyScript(), "1.2.3.4:25565") {
		t.Errorf("CopyScript = %s", row.CopyScript())
	}

	unknownRow := ServerRowUI{}
	if unknownRow.PlayersText() != "—" || unknownRow.UptimeText() != "—" {
		t.Errorf("expected — for unknown stats")
	}

	card := GameCardUI{Rows: []ServerRowUI{row, {Online: false}}}
	if card.OnlineCount() != 1 {
		t.Errorf("OnlineCount = %d, want 1", card.OnlineCount())
	}

	if got := ServersOnlineLabel(0); got != "Offline" {
		t.Errorf("ServersOnlineLabel(0) = %s, want Offline", got)
	}
	if got := ServersOnlineLabel(2); got != "2 online" {
		t.Errorf("ServersOnlineLabel(2) = %s, want 2 online", got)
	}

	pwd := ValheimWorldPasswordUI{Name: "Midgard", Password: "secret"}
	if !strings.Contains(pwd.CopyScript(), "secret") {
		t.Errorf("pwd CopyScript = %s", pwd.CopyScript())
	}

	inst := InstanceUI{Source: "modpack", Pack: "testpack"}
	if !inst.PackDefined() {
		t.Errorf("expected PackDefined = true")
	}
	instVanilla := InstanceUI{Source: "scratch"}
	if instVanilla.PackDefined() {
		t.Errorf("expected PackDefined = false")
	}
}

func TestSummaryAndActions(t *testing.T) {
	vhSum := ValheimSummaryUI{
		ActiveInstances: []InstanceUI{{Number: 1, Name: "V1"}},
	}
	if p := vhSum.Primary(); p == nil || p.Name != "V1" {
		t.Errorf("Primary = %v", p)
	}
	vhEmpty := ValheimSummaryUI{ActiveInstance: &InstanceUI{Name: "Fallback"}}
	if p := vhEmpty.Primary(); p == nil || p.Name != "Fallback" {
		t.Errorf("Primary fallback = %v", p)
	}

	mcSum := MinecraftSummaryUI{
		ActiveInstances: []InstanceUI{{Number: 1, Name: "M1"}},
	}
	if p := mcSum.Primary(); p == nil || p.Name != "M1" {
		t.Errorf("MC Primary = %v", p)
	}
	mcEmpty := MinecraftSummaryUI{ActiveInstance: &InstanceUI{Name: "FallbackM"}}
	if p := mcEmpty.Primary(); p == nil || p.Name != "FallbackM" {
		t.Errorf("MC Primary fallback = %v", p)
	}

	vActions := ValheimActions(vhSum)
	if len(vActions.Links) == 0 || vActions.Manager.Href == "" {
		t.Errorf("vActions invalid: %v", vActions)
	}
	vActionsEmpty := ValheimActions()
	if len(vActionsEmpty.Special) == 0 {
		t.Errorf("expected Special in empty vActions")
	}

	mcActions := MinecraftActions(mcSum)
	if len(mcActions.Links) == 0 || mcActions.Manager.Href == "" {
		t.Errorf("mcActions invalid: %v", mcActions)
	}
	mcActionsEmpty := MinecraftActions()
	if len(mcActionsEmpty.Special) == 0 {
		t.Errorf("expected Special in empty mcActions")
	}

	vCard := ValheimCard("1.2.3.4:2456", "node-1", true, vhSum)
	if vCard.Title == "" || len(vCard.Rows) == 0 {
		t.Errorf("vCard = %v", vCard)
	}
	vCardDefault := ValheimCard("1.2.3.4:2456", "", false)
	if vCardDefault.Title == "" {
		t.Errorf("vCardDefault = %v", vCardDefault)
	}

	mcCard := MinecraftCard(mcSum, true)
	if mcCard.Title == "" || len(mcCard.Rows) == 0 {
		t.Errorf("mcCard = %v", mcCard)
	}

	if ValheimNav().Href != "/valheim" || MinecraftNav().Href != "/minecraft" {
		t.Errorf("Nav helpers invalid")
	}
}

func TestStylesAndBadges(t *testing.T) {
	for _, k := range []string{"danger", "go", "special", "link", "default"} {
		if s := actionStyle(k); s == "" {
			t.Errorf("actionStyle(%s) empty", k)
		}
	}
	if extrasStyle("accent") == "" || extrasLabelStyle("accent") == "" || iconStyle("accent") == "" {
		t.Errorf("extras/icon styles empty")
	}

	if modBadge("vanilla", "modded") != "Vanilla" {
		t.Errorf("expected Vanilla")
	}
	if modBadge("modlist", "modded") != "modded" {
		t.Errorf("expected modded")
	}

	row := ServerRowUI{}
	if withModpack(row, "/dl", false).DownloadURL != "" {
		t.Errorf("expected empty DownloadURL when hasMods=false")
	}
	if withModpack(row, "/dl", true).DownloadURL != "/dl" {
		t.Errorf("expected /dl DownloadURL when hasMods=true")
	}

	tabsVanilla := InstanceTabs("minecraft", 1, "overview", true)
	for _, tab := range tabsVanilla {
		if tab.Label == "Mods" {
			t.Errorf("vanilla tabs should not contain Mods")
		}
	}
	tabsModded := InstanceTabs("minecraft", 1, "mods", false)
	foundMods := false
	for _, tab := range tabsModded {
		if tab.Label == "Mods" {
			foundMods = true
			if !tab.Active {
				t.Errorf("mods tab should be active")
			}
		}
	}
	if !foundMods {
		t.Errorf("modded tabs should contain Mods")
	}
}

func TestIncidentViewAndDetailUI(t *testing.T) {
	if IncidentView(nil) != nil {
		t.Errorf("IncidentView(nil) should be nil")
	}
	in := &domain.Incident{
		At:           time.Now(),
		Reason:       "CrashLoop",
		RestartCount: 2,
		ExitCode:     137,
		OOMKilled:    true,
		LogTail:      "stack trace",
	}
	iv := IncidentView(in)
	if iv == nil || iv.RestartCount != 2 || !iv.OOMKilled {
		t.Errorf("IncidentView = %v", iv)
	}

	mcDetail := InstanceDetailUI{
		InstanceUI: InstanceUI{GameID: "minecraft", Number: 1, Name: "MC", State: "running", Players: 2, PlayersKnown: true},
	}
	if mcDetail.GamePath() != "minecraft" || mcDetail.UpstreamCatalogName() != "Modrinth" {
		t.Errorf("MC GamePath/Catalog invalid")
	}

	vhDetail := InstanceDetailUI{
		InstanceUI: InstanceUI{GameID: "valheim", Number: 1, Name: "VH", State: "running", Players: 1, PlayersKnown: true},
	}
	if vhDetail.GamePath() != "valheim" || vhDetail.UpstreamCatalogName() != "Thunderstore" {
		t.Errorf("VH GamePath/Catalog invalid")
	}

	// updateClick & undoClick & confirmIfPlayers
	click1 := updateClick(vhDetail, "@post('/update/%d')")
	if !strings.Contains(click1, "confirm") || !strings.Contains(click1, "1 player is") {
		t.Errorf("click1 = %s", click1)
	}
	click2 := updateClick(mcDetail, "@post('/update/%d')")
	if !strings.Contains(click2, "confirm") || !strings.Contains(click2, "2 players are") {
		t.Errorf("click2 = %s", click2)
	}
	undo := undoClick(mcDetail)
	if !strings.Contains(undo, "confirm") {
		t.Errorf("undo = %s", undo)
	}

	stoppedDetail := InstanceDetailUI{
		InstanceUI: InstanceUI{State: "stopped", PlayersKnown: true, Players: 5},
	}
	if got := updateClick(stoppedDetail, "@post('%d')"); strings.Contains(got, "confirm") {
		t.Errorf("stopped instance should not prompt confirm: %s", got)
	}

	// checkSummary
	if s := checkSummary(InstanceDetailUI{UpdatesError: "err"}); s != "err" {
		t.Errorf("checkSummary error = %s", s)
	}
	if s := checkSummary(InstanceDetailUI{}); s != "Not checked yet." {
		t.Errorf("checkSummary empty = %s", s)
	}
	if s := checkSummary(InstanceDetailUI{ModsChecked: 5, ModsTotal: 5, UpdatesChecked: "1m"}); !strings.Contains(s, "Checked all 5 mods") {
		t.Errorf("checkSummary all = %s", s)
	}
	if s := checkSummary(InstanceDetailUI{ModsChecked: 3, ModsTotal: 5, UpdatesChecked: "1m"}); !strings.Contains(s, "Checked 3 of 5 mods") {
		t.Errorf("checkSummary partial = %s", s)
	}
}

