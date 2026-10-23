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

func TestAGameWhoseModsAgrelhaDeliversIsRefused(t *testing.T) {
	p := domain.ValheimProfile
	inst := domain.Instance{GameID: p.ID, Number: 1, Name: "X", Slug: "x", Host: "fogo"}
	_, err := New(p, "/srv/gamedata", func(domain.Instance) domain.RuntimeSpec { return domain.RuntimeSpec{Image: "x"} }).
		Render(inst, domain.ModList{})
	if err == nil {
		t.Errorf(`%s declares ModDelivery %q, so agrelha downloads its Mod list itself and the server needs an
install step RuntimeSpec cannot describe. Rendering it as a bare single container yields a world that
starts, reports healthy, and runs zero mods.`, p.ID, p.ModDelivery)
	}
}

func TestAGameWhoseImageInstallsItsModsIsRendered(t *testing.T) {
	p := domain.MinecraftProfile
	inst := domain.Instance{GameID: p.ID, Number: 1, Name: "X", Slug: "x", Host: "fogo"}
	files, err := New(p, "/srv/gamedata", func(domain.Instance) domain.RuntimeSpec {
		return domain.RuntimeSpec{Image: "itzg/minecraft-server:java25", Env: map[string]string{"TYPE": "NEOFORGE"}}
	}).Render(inst, domain.ModList{Primary: "fabric/sodium\n"})
	if err != nil {
		t.Fatalf(`%s declares ModDelivery %q - the server image installs the Mod list from its own environment,
which RuntimeSpec does carry - so it must render: %v`, p.ID, p.ModDelivery, err)
	}
	for _, want := range []string{ComposeFileName, ModsFileName, ConfigsFileName} {
		if _, ok := files[want]; !ok {
			t.Errorf(`rendered files %v are missing %q.

PutTree deletes every file in the directory it was not given, so a renderer that emits only the compose
file silently destroys the Mod list and the config Overrides on the next converge.`, keys(files), want)
		}
	}
	if !strings.Contains(string(files[ModsFileName]), "fabric/sodium") {
		t.Errorf("mods.yaml did not carry the Mod list:\n%s", files[ModsFileName])
	}
}

func TestASecondWorldOnTheSameHostGetsItsOwnPorts(t *testing.T) {
	spec := func(domain.Instance) domain.RuntimeSpec {
		return domain.RuntimeSpec{Image: "x", Ports: []domain.PortSpec{{Name: "game", Port: 25565, Protocol: "TCP"}}}
	}
	r := New(domain.MinecraftProfile, "/srv/gamedata", spec)

	first, err := r.Render(domain.Instance{GameID: domain.GameMinecraft, Number: 1, Name: "A", Slug: "a", Host: "fogo"}, domain.ModList{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Render(domain.Instance{GameID: domain.GameMinecraft, Number: 2, Name: "B", Slug: "b", Host: "fogo"}, domain.ModList{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first[ComposeFileName]), "25565:25565/tcp") {
		t.Errorf("instance 01 must keep the stock port:\n%s", first[ComposeFileName])
	}
	if strings.Contains(string(second[ComposeFileName]), "25565:25565/tcp") {
		t.Errorf(`instance 02 published the same host port as instance 01.

Two worlds on one Docker Host cannot both bind 25565: the second container fails to start, and the
failure surfaces as a world that simply never comes up.
%s`, second[ComposeFileName])
	}
}

func TestAWorldOnAGameWithNoPortStrideIsRefusedRatherThanColliding(t *testing.T) {
	p := domain.MinecraftProfile
	p.PortStride = 0
	spec := func(domain.Instance) domain.RuntimeSpec {
		return domain.RuntimeSpec{Image: "x", Ports: []domain.PortSpec{{Name: "game", Port: 25565, Protocol: "TCP"}}}
	}
	_, err := New(p, "/srv/gamedata", spec).
		Render(domain.Instance{GameID: p.ID, Number: 2, Name: "B", Slug: "b", Host: "fogo"}, domain.ModList{})
	if err == nil {
		t.Error("a game with no PortStride must refuse its second Docker-Host world rather than render a colliding one")
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
