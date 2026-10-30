package pages

import (
	"slices"
	"strings"
	"testing"
)

func tabLabels(game string) []string {
	var out []string
	for _, t := range InstanceTabs(game, 1, "overview", false) {
		out = append(out, t.Label)
	}
	return out
}

func has(labels []string, want string) bool {
	return slices.Contains(labels, want)
}

func TestGModIsNotOfferedTabsItHasNoCapabilityFor(t *testing.T) {
	labels := tabLabels("gmod")

	for _, absent := range []string{"Mods", "Configs"} {
		if has(labels, absent) {
			t.Errorf(`the Garry's Mod page offers a %q tab, but its profile declares no such capability.

Garry's Mod runs whatever one Steam Workshop collection contains: there is no per-mod browser and no
config editor behind that tab, so it can only lead somewhere empty. Tabs are derived from
GameProfile.Capabilities precisely so a game cannot advertise what it does not have.
got %v`, absent, labels)
		}
	}
	for _, present := range []string{"Overview", "Logs"} {
		if !has(labels, present) {
			t.Errorf("the Garry's Mod page is missing its %q tab; got %v", present, labels)
		}
	}
}

func TestOnlyAGameThatCanExecuteCommandsAdvertisesAConsole(t *testing.T) {
	gmod := tabLabels("gmod")
	if has(gmod, "Console & Logs") {
		t.Errorf(`Garry's Mod advertises "Console & Logs" while declaring Capabilities.Console false.

Nothing executes commands on a Garry's Mod world: administration happens in-game through ULX, and
agrelha deliberately relays no commands. A tab promising a console would be a dead input box.
got %v`, gmod)
	}
	if !has(gmod, "Logs") {
		t.Errorf("Garry's Mod lost its Logs tab entirely; got %v", gmod)
	}

	for _, game := range []string{"valheim", "minecraft"} {
		labels := tabLabels(game)
		if !has(labels, "Console & Logs") {
			t.Errorf("%s executes commands remotely and must keep its console tab; got %v", game, labels)
		}
	}
}

func TestAnUnknownGameStillGetsAUsableTabStrip(t *testing.T) {
	labels := tabLabels("doesnotexist")
	if len(labels) == 0 {
		t.Error("an unregistered game rendered no tabs at all, leaving a page with no navigation")
	}
	if !has(labels, "Overview") {
		t.Errorf("an unregistered game lost its Overview tab; got %v", labels)
	}
}

func TestAVanillaWorldHidesTheModsTab(t *testing.T) {
	var labels []string
	for _, tab := range InstanceTabs("valheim", 1, "overview", true) {
		labels = append(labels, tab.Label)
	}
	if has(labels, "Mods") {
		t.Errorf("a vanilla world offered a Mods tab; gaining mods is a new Instance, not an edit. got %v", labels)
	}
}

func TestTabHrefsPointAtTheGame(t *testing.T) {
	for _, tab := range InstanceTabs("gmod", 7, "console", false) {
		if !strings.HasPrefix(tab.Href, "/gmod/7/") {
			t.Errorf("tab %q links to %q, outside its own world", tab.Label, tab.Href)
		}
	}
}

func TestEveryTabGModOffersIsAcceptedByItsHandler(t *testing.T) {
	for _, tab := range InstanceTabs("gmod", 1, "overview", false) {
		slug := tab.Href[strings.LastIndex(tab.Href, "/")+1:]
		if !HasInstanceTab("gmod", slug, false) {
			t.Errorf(`the Garry's Mod tab bar offers %q (%s) but HasInstanceTab rejects %q.

The handler validates the :tab parameter with HasInstanceTab, so any tab the bar offers that it does
not accept is a link straight to a 404 - which is exactly what shipped: a Logs tab the handler
refused because its allowlist was hardcoded to "overview".`, tab.Label, tab.Href, slug)
		}
	}
}

func TestGModDoesNotAdvertiseFeaturesAgrelhaCannotDoForIt(t *testing.T) {
	labels := tabLabels("gmod")
	if has(labels, "Backups") {
		t.Error(`Garry's Mod offers a Backups tab, but agrelha registers no GMod backup routes at all -
its backups are taken by a host timer outside agrelha. The tab can only lead to an empty pane.`)
	}
	if has(labels, "Settings") {
		t.Error(`Garry's Mod offers a Settings tab before the settings form exists, so the link 404s.
Add "settings" to implementedTabs["gmod"] when the form lands, not before.`)
	}
}
