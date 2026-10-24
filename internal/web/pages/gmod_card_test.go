package pages

import (
	"strings"
	"testing"

	"agrelha/internal/domain"
)

func gmodProfileForTest() domain.GameProfile { return domain.GModProfile }

func gmodSummary() GameSummaryUI {
	return GameSummaryUI{
		TotalInstances: 1, MaxInstances: 4,
		ActiveInstances: []InstanceUI{{
			Number: 1, Name: "Lareira", LBIP: "192.168.20.31", Host: "fogo",
			Password: "pilinha", Gamemode: "sandbox", Map: "gm_construct",
		}},
	}
}

func TestAnAnonymousVisitorCanJoinAGModWorldFromSteam(t *testing.T) {
	card := GModCard(gmodSummary(), false)
	if len(card.Rows) != 1 {
		t.Fatalf("expected one world, got %d", len(card.Rows))
	}
	row := card.Rows[0]

	if row.JoinURL == "" {
		t.Error(`the public Garry's Mod card offers no Steam join link.

Garry's Mod falls through to GenericGameCard, which sets Address to the bare IP: no port, no
password and no steam:// link. A visitor who is not signed in has no way to join, which is the
whole point of the public hub.`)
	}
	if !strings.HasPrefix(row.JoinURL, "steam://connect/192.168.20.31:27015") {
		t.Errorf("join link does not address the world's host and port: %q", row.JoinURL)
	}
	if !strings.HasSuffix(row.Address, ":27015") {
		t.Errorf("the address shown omits the port players must connect to: %q", row.Address)
	}
}

func TestTheGModPasswordIsShownSoVisitorsCanGetIn(t *testing.T) {
	row := GModCard(gmodSummary(), false).Rows[0]
	if row.Password != "pilinha" {
		t.Errorf(`the join password is not shown to anonymous visitors (%q).

The server is reachable only on the local network, so the password is deliberately public here:
withholding it just means nobody can join.`, row.Password)
	}
	if !strings.HasSuffix(row.JoinURL, "/pilinha") {
		t.Errorf("the steam:// link omits the password, so Steam will prompt and most people will give up: %q", row.JoinURL)
	}
}

func TestAWorldWithNoPasswordStillGetsAJoinLink(t *testing.T) {
	s := gmodSummary()
	s.ActiveInstances[0].Password = ""
	row := GModCard(s, false).Rows[0]
	if row.JoinURL != "steam://connect/192.168.20.31:27015" {
		t.Errorf("an open world got a malformed join link: %q", row.JoinURL)
	}
}

func TestTheOverviewLinkStaysAdminOnly(t *testing.T) {
	if GModCard(gmodSummary(), false).Rows[0].OverviewURL != "" {
		t.Error("the public card linked an anonymous visitor into the admin overview page")
	}
	if GModCard(gmodSummary(), true).Rows[0].OverviewURL != "/gmod/1/overview" {
		t.Error("an admin lost the overview link from the hub")
	}
}

func TestGenericCardWouldNotHaveServedGMod(t *testing.T) {
	row := GenericGameCard(gmodProfileForTest(), gmodSummary(), false).Rows[0]
	if row.JoinURL != "" || strings.Contains(row.Address, ":") {
		t.Skip("GenericGameCard has grown join support; this guard can go")
	}
	if GModCard(gmodSummary(), false).Rows[0].JoinURL == "" {
		t.Error("GModCard must add what GenericGameCard cannot: a Steam join link with port and password")
	}
}
