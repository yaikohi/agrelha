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
	for line := range strings.SplitSeq(body, "\n") {
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
	if found := strings.Contains(script, "/config-mods"); !found {
		t.Error("reconciler no longer references /config-mods")
	}
}

func TestSlowFirstBootIsNotReportedUnhealthy(t *testing.T) {
	body := string(renderCompose(t, false)[ComposeFileName])

	if !strings.Contains(body, "start_period:") {
		t.Fatal("no start_period: a multi-GB SteamCMD download is marked unhealthy ~90s in, and the runtime reads health into Status.Available")
	}
}

func TestASecondWorldDoesNotClaimTheFirstWorldsPorts(t *testing.T) {
	inst := composeWorld(false)
	inst.Number = 2
	files, err := NewComposeRenderer("", "/srv/gamedata").Render(inst, domain.ModList{Primary: "x\n"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	body := string(files[ComposeFileName])

	for _, taken := range []string{"2456:2456/udp", "2457:2457/udp", "9001:9001/tcp"} {
		if strings.Contains(body, taken) {
			t.Errorf("instance 02 publishes %s, already held by instance 01; compose up fails with \"port is already allocated\"", taken)
		}
	}
	for _, want := range []string{"2458:2456/udp", "2459:2457/udp", "9002:9001/tcp"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing host mapping %s\n%s", want, body)
		}
	}
}

func TestChangingTheModListChangesTheComposeFile(t *testing.T) {
	r := NewComposeRenderer("", "/srv/gamedata")
	inst := composeWorld(false)

	before, err := r.Render(inst, domain.ModList{Primary: "Smoothbrain-Mining-1.0.0\n"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	after, err := r.Render(inst, domain.ModList{Primary: "Smoothbrain-Mining-1.1.0\n"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if string(before[ComposeFileName]) == string(after[ComposeFileName]) {
		t.Fatal("the compose file is byte-identical after a mod version change;\ncompose up -d then sees no change, the one-shot reconciler never re-runs,\nand the world keeps serving the old mods while the UI reports success")
	}
	if !strings.Contains(string(after[ComposeFileName]), modsDigestLabel) {
		t.Errorf("no %s label in the rendered file", modsDigestLabel)
	}
}

func TestAnUnchangedModListRendersIdentically(t *testing.T) {
	r := NewComposeRenderer("", "/srv/gamedata")
	inst := composeWorld(false)
	mods := domain.ModList{Primary: "Smoothbrain-Mining-1.0.0\n"}

	a, _ := r.Render(inst, mods)
	b, _ := r.Render(inst, mods)
	if string(a[ComposeFileName]) != string(b[ComposeFileName]) {
		t.Error("rendering is not deterministic; every converge would recreate the world for no reason")
	}
}

func TestComposeRenderCarriesTheDesiredState(t *testing.T) {
	files := renderCompose(t, false)

	for _, want := range []string{"mods.yaml", "configs.yaml"} {
		if _, ok := files[want]; !ok {
			t.Errorf("the Compose render omits %s.\nwriteManifests commits the rendered set with PutTree, which deletes every\nsibling it was not given - so omitting it deletes the world's desired state\nfrom git, and the next render has no mods at all", want)
		}
	}
}

func TestBothHostsRenderTheSameDesiredState(t *testing.T) {
	inst := composeWorld(false)
	mods := domain.ModList{Primary: "denikson-BepInExPack_Valheim-5.4.2202\n"}

	compose, err := NewComposeRenderer("", "/srv/gamedata").Render(inst, mods)
	if err != nil {
		t.Fatalf("compose render: %v", err)
	}
	k8s, err := Render(inst, mods.Primary, "", "valheim")
	if err != nil {
		t.Fatalf("k8s render: %v", err)
	}

	for _, name := range []string{"mods.yaml", "configs.yaml"} {
		if string(compose[name]) != string(k8s[name]) {
			t.Errorf("%s differs between Hosts; the desired state must not depend on where a world runs\ncompose:\n%s\nk8s:\n%s", name, compose[name], k8s[name])
		}
	}
}

func TestComposeStateFilesDoNotRenderEmptyNamespace(t *testing.T) {
	files := renderCompose(t, false)
	for _, name := range []string{"configs.yaml", "mods.yaml"} {
		content := string(files[name])
		if strings.Contains(content, "namespace:") {
			t.Errorf("%s rendered at compose path includes namespace field, which is invalid YAML when empty and leaks Kubernetes concerns into Docker Host desired state:\n%s", name, content)
		}
	}
}
