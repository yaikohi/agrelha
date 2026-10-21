package games_test

import (
	"strings"
	"testing"

	"agrelha/internal/app/games/gmod"
	"agrelha/internal/app/games/minecraft"
	"agrelha/internal/app/games/valheim"
	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

func envPaths(env map[string]string) []string {
	var out []string
	for _, v := range env {
		for tok := range strings.FieldsSeq(v) {
			tok = strings.TrimPrefix(tok, "@")
			if strings.HasPrefix(tok, "/") {
				out = append(out, tok)
			}
		}
	}
	return out
}

func covered(path string, vols []domain.VolumeSpec) bool {
	for _, v := range vols {
		if v.MountPath != "" && strings.HasPrefix(path, v.MountPath) {
			return true
		}
	}
	return false
}

func TestEveryPathAGamePutsInItsEnvIsBackedByAVolume(t *testing.T) {
	cases := []struct {
		name string
		game ports.Game
		inst domain.Instance
	}{
		{"minecraft", minecraft.New(), domain.Instance{
			GameID: domain.GameMinecraft, Number: 1, Name: "Flux", Slug: "flux",
			Source:    domain.SourceModlist,
			Minecraft: &domain.MinecraftConfig{Loader: domain.LoaderNeoForge, MCVersion: "1.21.1"},
		}},
		{"valheim", valheim.New(), domain.Instance{
			GameID: domain.GameValheim, Number: 1, Name: "Odin", Slug: "odin",
			Valheim: &domain.ValheimConfig{Password: "x"},
		}},
		{"gmod", gmod.New(), domain.Instance{
			GameID: domain.GameGMod, Number: 1, Name: "Lareira", Slug: "lareira",
			GMod: &domain.GModConfig{Gamemode: "sandbox", Map: "gm_construct"},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := tc.game.RuntimeSpec(tc.inst)
			for _, p := range envPaths(spec.Env) {
				if !covered(p, spec.Volumes) {
					t.Errorf("env references %q but no declared volume provides it; a runtime built from this spec would start and silently find nothing there", p)
				}
			}
		})
	}
}
