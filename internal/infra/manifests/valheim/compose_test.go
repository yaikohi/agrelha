package valheim

import (
	"strings"
	"testing"

	"agrelha/internal/domain"
)

func composeWorld(vanilla bool) domain.Instance {
	inst := domain.Instance{
		GameID: domain.GameValheim, Number: 1, Name: "Odin", Slug: "odin",
		Tier: domain.TierMedium, State: domain.StateRunning, Host: "fogo",
		Valheim: &domain.ValheimConfig{Password: "secret"},
	}
	if vanilla {
		inst.Source = domain.SourceVanilla
	}
	return inst
}

func renderCompose(t *testing.T, vanilla bool) map[string][]byte {
	t.Helper()
	files, err := NewComposeRenderer("", "/srv/gamedata").
		Render(composeWorld(vanilla), domain.ModList{Primary: "denikson-BepInExPack_Valheim-5.4.2202\n"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return files
}

func TestModdedWorldGetsAOneShotReconciler(t *testing.T) {
	body := string(renderCompose(t, false)[ComposeFileName])

	for _, want := range []string{
		"mod-reconcile:",
		"image: alpine:3.20",
		"depends_on:",
		"condition: service_completed_successfully",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("compose missing %q\n---\n%s", want, body)
		}
	}
}

func TestTheServerActuallyLoadsWhatTheReconcilerDownloaded(t *testing.T) {
	body := string(renderCompose(t, false)[ComposeFileName])

	if !strings.Contains(body, "PRE_SERVER_RUN_HOOK") {
		t.Fatal("no PRE_SERVER_RUN_HOOK: mods would be downloaded and never loaded")
	}
	if !strings.Contains(body, "/opt/valheim/bepinex/BepInEx/plugins") {
		t.Error("the hook must publish plugins into the path the server reads")
	}
	if !strings.Contains(body, "PRE_BEPINEX_CONFIG_HOOK") {
		t.Error("a modded world needs the BepInEx config hook")
	}
}

func TestVanillaWorldHasNoReconciler(t *testing.T) {
	files := renderCompose(t, true)
	body := string(files[ComposeFileName])

	if strings.Contains(body, "mod-reconcile") {
		t.Error("a Vanilla world must not get a reconciler service")
	}
	if strings.Contains(body, "depends_on") {
		t.Error("nothing to depend on for a Vanilla world")
	}
	if _, ok := files["mods.txt"]; ok {
		t.Error("a Vanilla world has no mod list")
	}
	if strings.Contains(body, "PRE_BEPINEX_CONFIG_HOOK") {
		t.Error("the BepInEx hook is meaningless without BepInEx")
	}
}

func TestComposeAndKubernetesShareTheHooks(t *testing.T) {
	compose := string(renderCompose(t, false)[ComposeFileName])

	k8s, err := New("ykhi.xyz/gameserver=true", "valheim").
		Render(composeWorld(false), domain.ModList{Primary: "x-y-1.0.0\n"})
	if err != nil {
		t.Fatal(err)
	}
	dep := string(k8s["deployment.yaml"])

	hook := Data{Instance: composeWorld(false), Profile: domain.ValheimProfile}.PreServerRunHook()
	if !strings.Contains(dep, hook) {
		t.Error("the Kubernetes deployment no longer carries the shared hook")
	}
	if !strings.Contains(compose, hook) {
		t.Error("the compose file no longer carries the shared hook; the two Hosts have diverged")
	}
}

func TestVolumesPointAtTheDataRoot(t *testing.T) {
	body := string(renderCompose(t, false)[ComposeFileName])

	if !strings.Contains(body, "/srv/gamedata/valheim/instance-01/config:/config") {
		t.Errorf("config volume not under the data root:\n%s", body)
	}
	for _, line := range strings.Split(body, "\n") {
		v := strings.TrimSpace(line)
		if !strings.HasPrefix(v, "- ./") {
			continue
		}
		if strings.HasPrefix(v, "- ./"+ModsFileName+":") {
			continue
		}
		t.Errorf("volume %q resolves beside the compose file, inside the tree materialised from git", v)
	}
}

func TestDataRootIsRequired(t *testing.T) {
	if _, err := NewComposeRenderer("", "").Render(composeWorld(false), domain.ModList{}); err == nil {
		t.Error("rendering without a data root must fail rather than place world data beside the compose file")
	}
}

func TestTheReconcilerCanActuallyReadTheModList(t *testing.T) {
	files := renderCompose(t, false)
	body := string(files[ComposeFileName])

	if _, ok := files[ModsFileName]; !ok {
		t.Fatalf("renderer emitted no %s", ModsFileName)
	}

	mount := "./" + ModsFileName + ":/config-mods/" + ModsFileName + ":ro"
	if !strings.Contains(body, mount) {
		t.Fatalf("the reconciler reads /config-mods/%s, and nothing mounts the rendered file there;\nthe world starts clean with zero mods and looks healthy\nwant volume %q\n---\n%s", ModsFileName, mount, body)
	}

	script := body[strings.Index(body, "mod-reconcile:"):]
	if i := strings.Index(script, "/config-mods"); i < 0 {
		t.Error("reconciler no longer references /config-mods")
	}
}

func TestSlowFirstBootIsNotReportedUnhealthy(t *testing.T) {
	body := string(renderCompose(t, false)[ComposeFileName])

	if !strings.Contains(body, "start_period:") {
		t.Fatal("no start_period: a multi-GB SteamCMD download is marked unhealthy ~90s in, and the runtime reads health into Status.Available")
	}
}
