package pages

import (
	"strings"
	"testing"
)

func TestAnAnonymousVisitorCanBrowseWhatAGModServerRuns(t *testing.T) {
	s := gmodSummary()
	s.ActiveInstances[0].CollectionID = "2270097915"
	s.ActiveInstances[0].CollectionItems = 297

	row := GModCard(s, false).Rows[0]
	if row.BrowseURL == "" {
		t.Error(`the public Garry's Mod card links nothing a visitor can look at.

agrelha already knows the Workshop collection, and it is public on Steam. Without the link the page
says a server exists but gives no way to find out what is on it - which is the whole gap.`)
	}
	if !strings.Contains(row.BrowseURL, "2270097915") {
		t.Errorf("browse link does not point at this world's collection: %q", row.BrowseURL)
	}
	if !strings.Contains(row.BrowseLabel, "297") {
		t.Errorf("the label hides how much is in there; got %q", row.BrowseLabel)
	}
}

func TestAWorldWithNoCollectionOffersNoBrowseLink(t *testing.T) {
	if GModCard(gmodSummary(), false).Rows[0].BrowseURL != "" {
		t.Error("a world with no collection got a link to nowhere")
	}
}

func TestAnOfflineMinecraftWorldIsStillDescribed(t *testing.T) {
	mc := GameSummaryUI{
		TotalInstances: 2, MaxInstances: 4,
		SavedInstances: []InstanceUI{{
			Number: 1, Name: "Fluxweave", Slug: "fluxweave", LBIP: "192.168.20.31",
			MCVersion: "1.21.1", Loader: "NeoForge", HasMods: true, Source: "modlist",
		}},
	}

	card := MinecraftCard(mc, false)
	if len(card.Rows) != 1 {
		t.Fatalf(`an offline Minecraft world produced %d rows.

The card only ever listed running worlds, so a visitor saw "All Minecraft worlds are currently
offline - ask the host on Discord" and nothing else: no version, no loader, no modpack. Valheim
offered a download for its worlds; Minecraft offered a shrug.`, len(card.Rows))
	}
	row := card.Rows[0]
	if row.Online {
		t.Error("a saved world was marked Online; players would try to join a server that is not up")
	}
	if !strings.Contains(row.VersionBadge, "1.21.1") || !strings.Contains(row.VersionBadge, "NeoForge") {
		t.Errorf("the offline world does not say what it is; badge was %q", row.VersionBadge)
	}
	if row.DownloadURL == "" {
		t.Error("a modded world offered no modpack download, though the export endpoint is public and works while stopped")
	}
}

func TestOfflineWorldsDoNotLeakAnAdminLink(t *testing.T) {
	mc := GameSummaryUI{SavedInstances: []InstanceUI{{Number: 1, Name: "X", HasMods: false}}}
	if MinecraftCard(mc, false).Rows[0].OverviewURL != "" {
		t.Error("the public card linked an anonymous visitor into the admin overview")
	}
}
