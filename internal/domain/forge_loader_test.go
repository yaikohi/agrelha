package domain

import "testing"

// Legacy Forge is a Loader in its own right. It used to collapse into NeoForge,
// which is how a Forge 1.20.1 Pack came to be recorded as something else.
func TestForgeIsItsOwnLoader(t *testing.T) {
	for _, in := range []string{"forge", "Forge", " FORGE "} {
		if got := NormalizeLoader(in); got != LoaderForge {
			t.Errorf("NormalizeLoader(%q) = %q, want forge", in, got)
		}
	}
	if got := NormalizeLoader("neoforge"); got != LoaderNeoForge {
		t.Errorf("NormalizeLoader(neoforge) = %q, want neoforge - forge must match the whole word", got)
	}
	if got := ParseLoader("forge"); got != LoaderForge {
		t.Errorf("ParseLoader(forge) = %q, want forge", got)
	}
}

func TestAForgeModlistWorldRunsForge(t *testing.T) {
	inst := Instance{
		GameID: GameMinecraft,
		Name:   "furunculus",
		Source: SourceModlist,
		Minecraft: &MinecraftConfig{
			Loader:    LoaderForge,
			MCVersion: "1.20.1",
		},
	}
	env := inst.Env(MinecraftProfile)
	if env["TYPE"] != "FORGE" {
		t.Errorf("TYPE = %q, want FORGE", env["TYPE"])
	}
	if env["FORGE_VERSION"] != "latest" {
		t.Errorf("FORGE_VERSION = %q, want latest", env["FORGE_VERSION"])
	}
	if _, ok := env["NEOFORGE_VERSION"]; ok {
		t.Error("a Forge world must not be handed NEOFORGE_VERSION")
	}
}

// A Pack Instance never gets a Loader in its env at all: the installer reads the
// Pack. This is why recording "forge" for furuncu cannot disturb what is running.
func TestAForgePackWorldKeepsAutoCurseForge(t *testing.T) {
	inst := Instance{
		GameID: GameMinecraft,
		Name:   "furunculus",
		Source: SourceModpack,
		Minecraft: &MinecraftConfig{
			Pack:   &Pack{Provider: ProviderCurseForge, Ref: "https://www.curseforge.com/minecraft/modpacks/furunculus"},
			Loader: LoaderForge,
		},
	}
	env := inst.Env(MinecraftProfile)
	if env["TYPE"] != "AUTO_CURSEFORGE" {
		t.Errorf("TYPE = %q, want AUTO_CURSEFORGE", env["TYPE"])
	}
	if _, ok := env["FORGE_VERSION"]; ok {
		t.Error("a Pack world must not be handed FORGE_VERSION")
	}
}
