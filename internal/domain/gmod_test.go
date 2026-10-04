package domain

import (
	"strings"
	"testing"
)

func TestGModWorldIsNotShapedLikeMinecraft(t *testing.T) {
	inst := Instance{
		GameID: GameGMod, Number: 2, Name: "TTT", Slug: "ttt",
		Tier: TierSmall, State: StateStopped,
		GMod: &GModConfig{Gamemode: "terrortown", Map: "gm_construct"},
	}

	if got := inst.DeploymentName(GModProfile); got != "gmod-ttt-02" {
		t.Errorf("deployment name = %q, want gmod-ttt-02", got)
	}

	env := inst.Env(GModProfile)
	for _, forbidden := range []string{"TYPE", "MODRINTH_PROJECTS", "MEMORY", "INIT_MEMORY", "MAX_MEMORY"} {
		if _, ok := env[forbidden]; ok {
			t.Errorf("Garry's Mod inherited the Minecraft variable %q", forbidden)
		}
	}
	for k := range env {
		if strings.Contains(strings.ToUpper(k), "JVM") {
			t.Errorf("Garry's Mod inherited a JVM variable %q", k)
		}
	}

	if inst.HeapInitMemoryGiB(GModProfile) != 0 {
		t.Error("Garry's Mod must have no JVM heap setting")
	}
}

func TestGModEnvMatchesTheImageContract(t *testing.T) {
	inst := Instance{
		GameID: GameGMod, Number: 1, Name: "TTT Night", Slug: "ttt", MaxPlayers: 24,
		GMod: &GModConfig{
			Pack:     &Pack{Provider: ProviderSteamWorkshop, Ref: "104604903"},
			Gamemode: "terrortown", Map: "ttt_minecraft_b5", Password: "hunter2",
		},
	}
	env := inst.Env(GModProfile)

	for k, want := range map[string]string{
		"NAME":       "TTT Night",
		"GAMEMODE":   "terrortown",
		"MAP":        "ttt_minecraft_b5",
		"MAXPLAYERS": "24",
		"PRODUCTION": "1",
	} {
		if env[k] != want {
			t.Errorf("env[%s] = %q, want %q", k, env[k], want)
		}
	}

	args := env["ARGS"]
	if !strings.Contains(args, "+host_workshop_collection 104604903") {
		t.Errorf("ARGS missing the workshop collection: %q", args)
	}
	if !strings.Contains(args, "+sv_password hunter2") {
		t.Errorf("ARGS missing the server password: %q", args)
	}

	for _, invented := range []string{"SERVER_NAME", "WORKSHOP_COLLECTION", "SV_PASSWORD", "HOSTNAME"} {
		if _, ok := env[invented]; ok {
			t.Errorf("env carries %q, which this image does not read", invented)
		}
	}
}

func TestGModTiersAreSmall(t *testing.T) {
	want := map[ResourceTier][2]int{
		TierSmall:  {2, 3},
		TierMedium: {4, 6},
		TierLarge:  {8, 10},
	}
	for tier, w := range want {
		got := BuiltinResources(GameGMod, tier)
		if got.MemRequestGiB != w[0] || got.MemLimitGiB != w[1] {
			t.Errorf("gmod %s = %d/%d GiB, want %d/%d",
				tier, got.MemRequestGiB, got.MemLimitGiB, w[0], w[1])
		}
	}

	tiers := DefaultTiersFor(GameGMod)
	if len(tiers) != 3 {
		t.Fatalf("expected 3 default tiers, got %d", len(tiers))
	}
	for _, tr := range tiers {
		if tr.HeapInitGiB != 0 {
			t.Errorf("tier %s carries a JVM heap size", tr.Key)
		}
	}
}

func TestGModCapabilities(t *testing.T) {
	c := GModProfile.Capabilities
	if c.Mods {
		t.Error("Garry's Mod has no per-mod browser; addons come from a collection")
	}
	if !c.Modpacks {
		t.Error("Garry's Mod runs a Workshop collection")
	}
	if c.Configs || c.Operators || c.PlayerCount {
		t.Error("configs, operators and player count are deliberately not supported yet")
	}
	if !c.Backups || !c.AdmissionPassword {
		t.Error("Garry's Mod supports backups and a server password")
	}
}
