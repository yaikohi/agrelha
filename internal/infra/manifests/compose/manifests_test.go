package compose

import (
	"strings"
	"testing"

	"agrelha/internal/domain"
)

func gmodSpec(inst domain.Instance) domain.RuntimeSpec {
	return domain.RuntimeSpec{
		Image: "ceifa/garrysmod@sha256:deadbeef",
		Ports: []domain.PortSpec{
			{Name: "game", Port: 27015, Protocol: "UDP"},
			{Name: "rcon", Port: 27015, Protocol: "TCP"},
		},
		Volumes:     []domain.VolumeSpec{{Name: "data", MountPath: "/home/gmod/server/garrysmod/data"}},
		Env:         inst.Env(domain.GModProfile),
		HealthProbe: "/home/gmod/health.sh",
	}
}

func gmodWorld() domain.Instance {
	return domain.Instance{
		GameID: domain.GameGMod, Number: 1, Name: "Lareira", Slug: "lareira",
		Tier: domain.TierMedium, State: domain.StateRunning, Host: "fogo",
		GMod: &domain.GModConfig{Gamemode: "sandbox", Map: "gm_construct"},
	}
}

func TestGModRendersAComposeFile(t *testing.T) {
	files, err := New(domain.GModProfile, "/srv/gamedata", gmodSpec).Render(gmodWorld(), domain.ModList{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	body, ok := files[ComposeFileName]
	if !ok {
		t.Fatalf("no %s produced, got %v", ComposeFileName, keys(files))
	}

	out := string(body)
	for _, want := range []string{
		"gmod-lareira-01:",
		"image: ceifa/garrysmod@sha256:deadbeef",
		"27015:27015/udp",
		"27015:27015/tcp",
		"/home/gmod/server/garrysmod/data",
		"GAMEMODE: sandbox",
		"healthcheck:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("compose file missing %q\n---\n%s", want, out)
		}
	}
}

func TestGamesNeedingModReconciliationAreRefused(t *testing.T) {
	for _, p := range []domain.GameProfile{domain.ValheimProfile, domain.MinecraftProfile} {
		inst := domain.Instance{GameID: p.ID, Number: 1, Name: "X", Slug: "x", Host: "fogo"}
		_, err := New(p, "/srv/gamedata", func(domain.Instance) domain.RuntimeSpec { return domain.RuntimeSpec{Image: "x"} }).
			Render(inst, domain.ModList{})
		if err == nil {
			t.Errorf("%s has per-mod installs and must be refused until its compose shape is designed, not rendered as a bare single container", p.ID)
		}
	}
}

func TestMissingSpecSourceIsRefused(t *testing.T) {
	if _, err := New(domain.GModProfile, "/srv/gamedata", nil).Render(gmodWorld(), domain.ModList{}); err == nil {
		t.Error("a renderer with no RuntimeSpec source must fail loudly, not emit an empty compose file")
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
