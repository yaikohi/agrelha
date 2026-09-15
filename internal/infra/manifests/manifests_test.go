package manifests

import (
	"agrelha/internal/domain"
	"strings"
	"testing"
)

func TestRenderInstanceManifests(t *testing.T) {
	inst := domain.Instance{
		Number:     1,
		Name:       "Fluxweave",
		Slug:       "fluxweave",
		Seed:       "123456789",
		Loader:     domain.LoaderNeoForge,
		Source:     domain.SourceModlist,
		MCVersion:  "1.21.1",
		Tier:       domain.TierMedium,
		State:      domain.StateRunning,
		Difficulty: "hard",
		Gamemode:   "survival",
		// Explicit: with no MC_LB_BASE_IP configured, agrelha pins no address
		// and lets the load balancer allocate one.
		LBIP: "192.168.20.225",
	}

	files, err := Render(inst, "jei\nappleskin\n", "ykhi.xyz/gameserver=true", "minecraft-modded")
	if err != nil {
		t.Fatalf("unexpected error rendering manifests: %v", err)
	}

	expectedFiles := []string{"deployment.yaml", "service.yaml", "pvc.yaml", "slot.yaml", "mods.yaml"}
	for _, f := range expectedFiles {
		content, ok := files[f]
		if !ok {
			t.Errorf("missing expected file %s", f)
			continue
		}
		if len(content) == 0 {
			t.Errorf("file %s is empty", f)
		}
	}

	dep := string(files["deployment.yaml"])
	if !strings.Contains(dep, "name: mc-fluxweave-01") {
		t.Errorf("deployment does not contain expected name: %s", dep)
	}
	if !strings.Contains(dep, "replicas: 1") {
		t.Errorf("running deployment should have replicas: 1: %s", dep)
	}
	if !strings.Contains(dep, "claimName: mc-instance-01-data") {
		t.Errorf("deployment should mount mc-instance-01-data: %s", dep)
	}

	svc := string(files["service.yaml"])
	if !strings.Contains(svc, "192.168.20.225") {
		t.Errorf("service should have LB IP 192.168.20.225: %s", svc)
	}
}

func TestMinecraftReadinessUsesMCHealth(t *testing.T) {
	inst := domain.Instance{
		Number: 1, Name: "almere", Slug: "almere",
		Tier: domain.TierMedium, State: domain.StateRunning,
		Loader: domain.LoaderNeoForge, MCVersion: "1.21.1",
	}

	files, err := Render(inst, "", "", "minecraft-modded")
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	dep := string(files["deployment.yaml"])

	if strings.Contains(dep, "tcpSocket") {
		t.Error("tcpSocket only proves the port is bound, not that the server answers")
	}
	if strings.Count(dep, `command: ["mc-health"]`) != 2 {
		t.Errorf("both startup and readiness probes must use mc-health (the probe domain.RuntimeSpec already declares): %s", dep)
	}
}

func TestRenderer_FabricAndStopped(t *testing.T) {
	r := New("topology.kubernetes.io/zone=us-east-1a", "minecraft-modded")
	inst := domain.Instance{
		Number:    2,
		Name:      "Fabric World",
		Slug:      "fabric-world",
		Loader:    domain.LoaderFabric,
		Source:    domain.SourceModlist,
		MCVersion: "1.20.1",
		Tier:      domain.TierSmall,
		State:     domain.StateStopped,
	}

	files, err := r.Render(inst, "fabric-api\nsodium\n")
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	dep := string(files["deployment.yaml"])
	if !strings.Contains(dep, "replicas: 0") {
		t.Errorf("stopped instance should have replicas: 0, got %s", dep)
	}
	slot := string(files["slot.yaml"])
	if !strings.Contains(slot, "TYPE: \"FABRIC\"") {
		t.Errorf("fabric instance should have TYPE FABRIC in slot.yaml, got %s", slot)
	}

	// Test IndentModsTxt
	d := Data{ModsTxt: "mod1\nmod2"}
	indented := d.IndentModsTxt()
	if indented != "    mod1\n    mod2\n" {
		t.Errorf("unexpected IndentModsTxt: %q", indented)
	}

	// Test SourceVanilla omits mods.yaml
	instVanilla := domain.Instance{
		Number:    3,
		Name:      "Vanilla Server",
		Slug:      "vanilla-server",
		Source:    domain.SourceVanilla,
		MCVersion: "1.21.1",
		Tier:      domain.TierSmall,
	}
	filesV, err := Render(instVanilla, "", "", "")
	if err != nil {
		t.Fatalf("Render vanilla failed: %v", err)
	}
	if _, ok := filesV["mods.yaml"]; ok {
		t.Error("expected mods.yaml to be omitted for vanilla instance with no mods")
	}
	if !strings.Contains(string(filesV["deployment.yaml"]), "namespace: minecraft-modded") {
		t.Errorf("expected default namespace minecraft-modded, got: %s", string(filesV["deployment.yaml"]))
	}

	// Test SourceFabric with empty modsTxt generates default mod list
	filesF, err := Render(inst, "", "", "minecraft-modded")
	if err != nil {
		t.Fatalf("Render fabric empty mods failed: %v", err)
	}
	if modsF, ok := filesF["mods.yaml"]; !ok || !strings.Contains(string(modsF), "# Mod list for") {
		t.Errorf("expected default mods placeholder in mods.yaml, got %s", string(modsF))
	}
}

