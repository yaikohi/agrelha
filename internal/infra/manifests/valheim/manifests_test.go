package valheim

import (
	"strings"
	"testing"

	"agrelha/internal/domain"
)

func TestValheimRenderBasic(t *testing.T) {
	inst := domain.Instance{
		GameID:   domain.GameValheim,
		Number:   1,
		Name:     "Odin's Domain",
		Slug:     "odins-domain",
		Password: "secretpassword",
		Seed:     "valheim123",
		Tier:     domain.TierMedium,
		State:    domain.StateRunning,
		LBIP:     "192.168.20.210",
	}

	renderer := New("dedicated=gameserver", "valheim")
	files, err := renderer.Render(inst, "denikson/BepInExPack_Valheim\nvalheim/jotunn\n")
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	expectedFiles := []string{"deployment.yaml", "service.yaml", "pvc.yaml", "slot.yaml", "configs.yaml", "mods.yaml"}
	for _, f := range expectedFiles {
		if _, ok := files[f]; !ok {
			t.Errorf("missing expected file: %s", f)
		}
	}

	// 1. Verify deployment.yaml
	dep := string(files["deployment.yaml"])
	if !strings.Contains(dep, "name: valheim-odins-domain-01") {
		t.Errorf("deployment name missing or wrong: %s", dep)
	}
	if !strings.Contains(dep, "replicas: 1") {
		t.Errorf("expected replicas: 1 for running state")
	}
	if !strings.Contains(dep, "image: lloesche/valheim-server:latest") {
		t.Errorf("deployment image missing: %s", dep)
	}
	if !strings.Contains(dep, "claimName: valheim-instance-01-data") {
		t.Errorf("PVC claimName missing or wrong: %s", dep)
	}
	if !strings.Contains(dep, "memory: 6Gi") {
		t.Errorf("expected 6Gi memory request for medium tier: %s", dep)
	}
	if !strings.Contains(dep, "memory: 7Gi") {
		t.Errorf("expected 7Gi memory limit for medium tier: %s", dep)
	}
	if !strings.Contains(dep, "dedicated: \"gameserver\"") {
		t.Errorf("nodeSelector missing in deployment: %s", dep)
	}

	// 2. Verify service.yaml
	svc := string(files["service.yaml"])
	if !strings.Contains(svc, "name: valheim-odins-domain-01") {
		t.Errorf("service name missing: %s", svc)
	}
	if !strings.Contains(svc, "io.cilium/lb-ipam-ips: \"192.168.20.210\"") {
		t.Errorf("cilium LB IP annotation missing: %s", svc)
	}
	if !strings.Contains(svc, "port: 2456") || !strings.Contains(svc, "protocol: UDP") {
		t.Errorf("UDP port 2456 missing in service: %s", svc)
	}
	if !strings.Contains(svc, "port: 2457") {
		t.Errorf("UDP port 2457 missing in service: %s", svc)
	}

	// 3. Verify pvc.yaml
	pvc := string(files["pvc.yaml"])
	if !strings.Contains(pvc, "name: valheim-instance-01-data") {
		t.Errorf("PVC name unexpected: %s", pvc)
	}

	// 4. Verify slot.yaml
	slot := string(files["slot.yaml"])
	if !strings.Contains(slot, "name: valheim-odins-domain-01-slot") {
		t.Errorf("slot CM name unexpected: %s", slot)
	}
	if !strings.Contains(slot, "SERVER_NAME: \"Odin's Domain\"") {
		t.Errorf("SERVER_NAME missing in slot CM: %s", slot)
	}
	if !strings.Contains(slot, "SERVER_PASS: \"secretpassword\"") {
		t.Errorf("SERVER_PASS missing in slot CM: %s", slot)
	}
	if !strings.Contains(slot, "WORLD_SEED: \"valheim123\"") {
		t.Errorf("WORLD_SEED missing in slot CM: %s", slot)
	}

	// 5. Verify mods.yaml
	mods := string(files["mods.yaml"])
	if !strings.Contains(mods, "valheim/jotunn") {
		t.Errorf("mods.txt content missing in mods CM: %s", mods)
	}
}

func TestValheimRenderStoppedSeedsEmptyModList(t *testing.T) {
	inst := domain.Instance{
		GameID: domain.GameValheim,
		Number: 2,
		Name:   "Valheim Vanilla",
		Slug:   "valheim-vanilla",
		Tier:   domain.TierLarge,
		State:  domain.StateStopped,
	}

	renderer := New("", "")
	files, err := renderer.Render(inst, "")
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	mods, ok := files["mods.yaml"]
	if !ok {
		t.Fatal("mods.yaml must be rendered even with no mods: the install path patches this file, and it cannot patch what does not exist")
	}
	if !strings.Contains(string(mods), "# Mod list for Valheim Vanilla") {
		t.Errorf("expected a placeholder mod list, got: %s", mods)
	}
	if !strings.Contains(string(mods), "name: valheim-valheim-vanilla-02-mods") {
		t.Errorf("expected the instance ModsCMName, got: %s", mods)
	}

	dep := string(files["deployment.yaml"])
	if !strings.Contains(dep, "replicas: 0") {
		t.Errorf("expected replicas: 0 for stopped instance: %s", dep)
	}
	if !strings.Contains(dep, "memory: 8Gi") {
		t.Errorf("expected 8Gi request for large tier: %s", dep)
	}
	if !strings.Contains(dep, "memory: 10Gi") {
		t.Errorf("expected 10Gi limit for large tier: %s", dep)
	}
}

func TestValheimDeploymentReconcilesMods(t *testing.T) {
	inst := domain.Instance{
		GameID: domain.GameValheim,
		Number: 2,
		Name:   "boppo",
		Slug:   "boppo",
		Tier:   domain.TierLarge,
		State:  domain.StateRunning,
	}

	files, err := New("", "").Render(inst, "")
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	dep := string(files["deployment.yaml"])

	if !strings.Contains(dep, "name: mod-reconciler") {
		t.Fatal("deployment must run the mod reconciler: lloesche's image cannot install Thunderstore mods itself")
	}
	if !strings.Contains(dep, "MODS=/config-mods/mods.txt") {
		t.Error("reconciler must read the mod list from the mounted ConfigMap")
	}
	if !strings.Contains(dep, "thunderstore.io/package/download/") {
		t.Error("reconciler must fetch packages from Thunderstore")
	}
	if !strings.Contains(dep, "name: valheim-boppo-02-mods") {
		t.Error("deployment must mount this instance's mods ConfigMap")
	}
}

func TestValheimReadinessChecksTheGamePort(t *testing.T) {
	inst := domain.Instance{
		GameID: domain.GameValheim, Number: 1, Name: "lareira", Slug: "lareira",
		Tier: domain.TierLarge, State: domain.StateRunning,
	}

	files, err := New("", "").Render(inst, "")
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	dep := string(files["deployment.yaml"])

	if strings.Contains(dep, "pgrep -f valheim_server") {
		t.Error("pgrep only proves the process exists: it reported Ready for ~3 min with no game ports bound during the 1.0 upgrade")
	}
	if !strings.Contains(dep, ":2456[[:space:]]") {
		t.Error("readiness must check that the game port is actually bound")
	}
}
