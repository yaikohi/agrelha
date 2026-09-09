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
