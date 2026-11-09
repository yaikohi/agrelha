package wiring

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/infra/installed"
	"agrelha/internal/platform/config"
)

func mcWorldOn(host domain.Host) domain.Instance {
	return domain.Instance{
		GameID: domain.GameMinecraft, Number: 2, Name: "Valeria", Slug: "valeria", Host: host,
		Minecraft: &domain.MinecraftConfig{Loader: domain.LoaderNeoForge, MCVersion: "26.1.2"},
	}
}

func dataRootWithPassword(t *testing.T, password string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "minecraft", domain.InstanceDirName(2), "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".rcon-cli.env"), []byte("password="+password+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// With no password configured the panel had no console at all, so a world with
// two people on it reported nobody on. The password the server generated for
// itself is readable beside the world.
func TestADockerWorldIsReachedWithThePasswordItGeneratedForItself(t *testing.T) {
	root := dataRootWithPassword(t, "Jb3kd92ksl")
	cfg := &config.Config{Runtime: "docker", DockerHostName: "fogo"}
	d := Deps{Installed: installed.New(root)}

	targets := mcRconTargets(cfg, d, mcWorldOn("fogo"))

	if len(targets) != 1 {
		t.Fatalf("targets = %+v, want exactly the world's own published port", targets)
	}
	if targets[0].password != "Jb3kd92ksl" {
		t.Errorf("password = %q, want the one the server wrote", targets[0].password)
	}
	port, _ := domain.HostPortFor(domain.MinecraftProfile, domain.Host("fogo"), 2, domain.MinecraftRconPort)
	if want := fmt.Sprintf("127.0.0.1:%d", port); targets[0].addr != want {
		t.Errorf("addr = %q, want %q", targets[0].addr, want)
	}
}

// A world that still claims the cluster is tried locally first: dialing a
// service DNS name that cannot resolve costs a timeout on every command, and
// that delay is paid by every page that counts players.
func TestAClusterWorldOnDockerIsTriedLocallyFirst(t *testing.T) {
	root := dataRootWithPassword(t, "world-own")
	cfg := &config.Config{
		Runtime: "docker", DockerHostName: "fogo",
		MinecraftRconPassword: "configured", MinecraftNamespace: "minecraft-modded",
	}
	inst := mcWorldOn(domain.HostCluster)
	inst.LBIP = "192.168.20.31"

	targets := mcRconTargets(cfg, Deps{Installed: installed.New(root)}, inst)

	if len(targets) < 2 {
		t.Fatalf("targets = %+v, want the local port and the cluster addresses", targets)
	}
	if got := targets[0].addr; !strings.HasPrefix(got, "127.0.0.1:") {
		t.Errorf("first target = %q, want the world's own published port", got)
	}
	if targets[0].password != "configured" {
		t.Errorf("a configured password must win over the generated one, got %q", targets[0].password)
	}
}

// A Docker world must never be dialed at a cluster address: it cannot answer
// there, and waiting for it to time out is time the page spends not rendering.
func TestADockerWorldIsNeverDialedAtAClusterAddress(t *testing.T) {
	root := dataRootWithPassword(t, "world-own")
	cfg := &config.Config{
		Runtime: "docker", DockerHostName: "fogo",
		MinecraftRconPassword: "configured", MinecraftNamespace: "minecraft-modded",
	}
	inst := mcWorldOn("fogo")
	inst.LBIP = "192.168.20.31"

	for _, target := range mcRconTargets(cfg, Deps{Installed: installed.New(root)}, inst) {
		if !strings.HasPrefix(target.addr, "127.0.0.1:") {
			t.Errorf("target %q is not the world's own published port", target.addr)
		}
	}
}

// Nothing to dial is better than dialing with a password that cannot work: the
// caller reports a console it could not reach rather than a count of zero.
func TestAWorldWithNoPasswordAnywhereHasNoTarget(t *testing.T) {
	cfg := &config.Config{Runtime: "docker", DockerHostName: "fogo"}
	if targets := mcRconTargets(cfg, Deps{}, mcWorldOn("fogo")); len(targets) != 0 {
		t.Errorf("targets = %+v, want none", targets)
	}
}
