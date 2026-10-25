package domain

import "testing"

func TestEveryGameAgreesOnTheWallClock(t *testing.T) {
	cases := []struct {
		name    string
		profile GameProfile
		inst    Instance
	}{
		{"valheim", ValheimProfile, Instance{GameID: GameValheim, Number: 1, Name: "V", Slug: "v", Valheim: &ValheimConfig{}}},
		{"gmod", GModProfile, Instance{GameID: GameGMod, Number: 1, Name: "G", Slug: "g", GMod: &GModConfig{Gamemode: "sandbox", Map: "gm_construct"}}},
	}
	for _, c := range cases {
		tz := c.inst.Env(c.profile)["TZ"]
		if tz != "Europe/Amsterdam" {
			t.Errorf(`%s runs on TZ %q, not the same wall clock as the other games.

A server with no TZ runs UTC, so its in-game clock and its own logs disagree with the people playing
on it - "ulx thetime" reported 05:04 PM when the operator's clock said 19:04.`, c.name, tz)
		}
	}
}
