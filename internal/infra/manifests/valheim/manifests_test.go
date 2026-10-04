package valheim

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"agrelha/internal/domain"
)

func TestValheimRenderBasic(t *testing.T) {
	inst := domain.Instance{
		GameID:   domain.GameValheim,
		Number:   1,
		Name:     "Odin's Domain",
		Slug:     "odins-domain",
		Valheim: &domain.ValheimConfig{
			Password: "secretpassword",
			Seed:     "valheim123",
		},
		Tier:     domain.TierMedium,
		State:    domain.StateRunning,
		LBIP:     "192.168.20.210",
	}

	renderer := New("dedicated=gameserver", "valheim")
	files, err := renderer.Render(inst, domain.ModList{Primary: "denikson/BepInExPack_Valheim\nvalheim/jotunn\n"})
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
	if !strings.Contains(dep, "image: lloesche/valheim-server@sha256:") {
		t.Errorf("deployment image must be digest-pinned, not tagged: %s", dep)
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
	if !strings.Contains(dep, "name: valheim-admins") {
		t.Errorf("valheim-admins configMapRef missing in deployment: %s", dep)
	}
	if !strings.Contains(dep, "name: POST_BOOTSTRAP_HOOK") {
		t.Errorf("POST_BOOTSTRAP_HOOK missing in deployment: %s", dep)
	}
	if !strings.Contains(dep, "name: PRE_BEPINEX_CONFIG_HOOK") {
		t.Errorf("PRE_BEPINEX_CONFIG_HOOK missing in modded deployment: %s", dep)
	}
	if !strings.Contains(dep, "name: PRE_SERVER_RUN_HOOK") || !strings.Contains(dep, "rsync -a --delete /config/bepinex/plugins/") {
		t.Errorf("PRE_SERVER_RUN_HOOK missing or not syncing plugins: %s", dep)
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
	files, err := renderer.Render(inst, domain.ModList{})
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

	files, err := New("", "").Render(inst, domain.ModList{})
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

	files, err := New("", "").Render(inst, domain.ModList{})
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

func TestValheimModConfigsGoWhereBepInExActuallyReads(t *testing.T) {
	inst := domain.Instance{
		GameID: domain.GameValheim, Number: 2, Name: "boppo", Slug: "boppo",
		Source: domain.SourceModlist, Tier: domain.TierLarge, State: domain.StateRunning,
	}

	files, err := New("", "").Render(inst, domain.ModList{Primary: "a-b-1.0.0\n"})
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	dep := string(files["deployment.yaml"])

	// /opt/valheim/bepinex/BepInEx/config is a symlink to /config/bepinex, so
	// /config/bepinex/config is one level below anything BepInEx reads. Syncing
	// there left every committed value unapplied for a month without any error.
	if strings.Contains(dep, "/config/bepinex/config") {
		t.Error("overrides must be written to /config/bepinex, not the /config/bepinex/config subdirectory BepInEx never reads")
	}
	if !strings.Contains(dep, "--out=/config/bepinex") {
		t.Error("the merge must write where BepInEx actually reads")
	}
	if !strings.Contains(dep, "--overrides=/custom-configs") {
		t.Error("the merge must read the operator's override sets from the mounted ConfigMap")
	}
	if !strings.Contains(dep, `"bepinex", "merge"`) {
		t.Error("the merge runs the agrelha binary, not a shell script: 207 KB files through sh quoting corrupt configs")
	}
}

func TestValheimDeploymentPublishesWhatAgrelhaCannotSeeItself(t *testing.T) {
	inst := domain.Instance{
		GameID: domain.GameValheim, Number: 2, Name: "boppo", Slug: "boppo",
		Source: domain.SourceModlist, Tier: domain.TierLarge, State: domain.StateRunning,
	}

	files, err := New("", "").Render(inst, domain.ModList{})
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	dep := string(files["deployment.yaml"])

	// agrelha has no pods/exec in the valheim namespace, so anything it needs to
	// know about the inside of the pod has to be published to the shared backups
	// export. A rendered deployment without these silently produces a world that
	// reports no server updates and no occupancy.
	if !strings.Contains(dep, "name: build-watch") {
		t.Error("build-watch sidecar missing: agrelha would never learn a server update is pending")
	}
	if !strings.Contains(dep, "/backups/boppo-02/server-build.json") {
		t.Error("build id must be published under the instance's own backups subdirectory")
	}
	if !strings.Contains(dep, `value: "/backups/boppo-02"`) {
		t.Error("BACKUPS_DIRECTORY must point off-node, or a lost disk takes the world and every backup with it")
	}
	if !strings.Contains(dep, "STATUS_HTTP_PORT") {
		t.Error("the served status port must match the declared containerPort, or occupancy is unreadable")
	}
}

// The templates carry multi-line shell, and a mis-indented heredoc renders
// output that only fails at ArgoCD sync time - long after the commit, on a
// world that is already down. Parse everything we emit, for both shapes.
func TestValheimRenderedManifestsAreValidYAML(t *testing.T) {
	for _, tc := range []struct {
		name string
		inst domain.Instance
		mods domain.ModList
	}{
		{"modded", domain.Instance{
			GameID: domain.GameValheim, Number: 2, Name: "boppo", Slug: "boppo",
			Source: domain.SourceModlist, Tier: domain.TierLarge, State: domain.StateRunning,
		}, domain.ModList{Primary: "Author-Mod-1.2.3\nOther-Thing-0.1.0\n"}},
		{"vanilla", domain.Instance{
			GameID: domain.GameValheim, Number: 1, Name: "lareira", Slug: "lareira-v2",
			Source: domain.SourceVanilla, Tier: domain.TierLarge, State: domain.StateRunning,
		}, domain.ModList{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, err := New("", "valheim").Render(tc.inst, tc.mods)
			if err != nil {
				t.Fatalf("Render failed: %v", err)
			}
			if len(files) == 0 {
				t.Fatal("Render produced no files")
			}
			for name, body := range files {
				var doc any
				if err := yaml.Unmarshal(body, &doc); err != nil {
					t.Errorf("%s is not valid YAML: %v\n%s", name, err, body)
				}
			}
		})
	}
}

func TestValheimRender_Stopped_And_Vanilla(t *testing.T) {
	inst := domain.Instance{
		GameID: domain.GameValheim,
		Number: 3,
		Name:   "Vanilla World",
		Slug:   "vanilla-world",
		Source: domain.SourceVanilla,
		Tier:   domain.TierSmall,
		State:  domain.StateStopped,
	}

	files, err := New("key=val", "valheim").Render(inst, domain.ModList{Primary: "some-mod\n"})
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	dep := string(files["deployment.yaml"])
	if !strings.Contains(dep, "replicas: 0") {
		t.Errorf("stopped instance should have replicas: 0, got %s", dep)
	}
	if !strings.Contains(dep, "name: valheim-admins") {
		t.Errorf("valheim-admins configMapRef missing in vanilla deployment: %s", dep)
	}
	if !strings.Contains(dep, "name: POST_BOOTSTRAP_HOOK") {
		t.Errorf("POST_BOOTSTRAP_HOOK missing in vanilla deployment: %s", dep)
	}
	if strings.Contains(dep, "PRE_BEPINEX_CONFIG_HOOK") {
		t.Errorf("PRE_BEPINEX_CONFIG_HOOK should not exist in vanilla deployment: %s", dep)
	}
	if !strings.Contains(dep, "name: PRE_SERVER_RUN_HOOK") || strings.Contains(dep, "rsync -a --delete /config/bepinex/plugins/") {
		t.Errorf("PRE_SERVER_RUN_HOOK unexpected in vanilla deployment: %s", dep)
	}

	// IndentModsTxt
	d := Data{ModsTxt: "vmod1\nvmod2"}
	if indented := d.IndentModsTxt(); indented != "    vmod1\n    vmod2\n" {
		t.Errorf("unexpected IndentModsTxt: %q", indented)
	}
}

func TestBuildWatchRejectsAnInProgressDownload(t *testing.T) {
	inst := domain.Instance{
		GameID: domain.GameValheim, Number: 2, Name: "boppo", Slug: "boppo",
		Source: domain.SourceModlist, Tier: domain.TierLarge, State: domain.StateRunning,
	}
	files, err := New("", "").Render(inst, domain.ModList{})
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	dep := string(files["deployment.yaml"])

	if !strings.Contains(dep, `[ "$installed" = "0" ]`) {
		t.Error("Steam writes buildid 0 while downloading; publishing that reads as 'update available, running build 0' for the whole 30 minute sleep")
	}
}
