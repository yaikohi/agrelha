package gmod

import (
	"sort"
	"strings"
	"testing"

	"agrelha/internal/domain"
)

func world() domain.Instance {
	return domain.Instance{
		GameID: domain.GameGMod, Number: 2, Name: "TTT Night", Slug: "ttt",
		Tier: domain.TierMedium, State: domain.StateRunning, MaxPlayers: 24,
		LBIP: "192.168.20.225",
		GMod: &domain.GModConfig{
			Pack:     &domain.Pack{Provider: domain.ProviderSteamWorkshop, Ref: "104604903"},
			Gamemode: "terrortown", Map: "ttt_minecraft_b5", Password: "hunter2",
		},
	}
}

func render(t *testing.T, inst domain.Instance) map[string][]byte {
	t.Helper()
	files, err := New("ykhi.xyz/gameserver=true", "gmod").Render(inst, domain.ModList{})
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	return files
}

func TestRendersExactlyTheGModFileSet(t *testing.T) {
	files := render(t, world())

	var got []string
	for name := range files {
		got = append(got, name)
	}
	sort.Strings(got)

	want := []string{"deployment.yaml", "pvc.yaml", "service.yaml", "slot.yaml"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("file set = %v, want %v", got, want)
	}
}

func TestDeploymentIsNotShapedLikeMinecraft(t *testing.T) {
	dep := string(render(t, world())["deployment.yaml"])

	if !strings.Contains(dep, "name: gmod-ttt-02") {
		t.Error("deployment is not named gmod-ttt-02")
	}
	for _, forbidden := range []string{"JVM", "MODRINTH", "INIT_MEMORY", "MAX_MEMORY", "bepinex", "valheim", "minecraft"} {
		if strings.Contains(dep, forbidden) {
			t.Errorf("deployment carries %q, which belongs to another game", forbidden)
		}
	}
}

func TestImageIsPinnedByDigest(t *testing.T) {
	dep := string(render(t, world())["deployment.yaml"])

	if !strings.Contains(dep, "image: "+Image) {
		t.Errorf("deployment does not use the pinned image %q", Image)
	}
	if !strings.Contains(Image, "@sha256:") {
		t.Errorf("image %q is not pinned by digest", Image)
	}
}

func TestDeploymentRendersInstanceResources(t *testing.T) {
	inst := world()
	inst.Resources = domain.Resources{
		MemRequestGiB: 4, MemLimitGiB: 6,
		CPURequestMilli: 1000, CPULimitMilli: 2000,
	}
	want := strings.Join([]string{
		"          resources:",
		"            requests:",
		"              cpu: 1000m",
		"              memory: 4Gi",
		"            limits:",
		"              cpu: 2000m",
		"              memory: 6Gi",
	}, "\n")

	if got := string(render(t, inst)["deployment.yaml"]); !strings.Contains(got, want) {
		t.Errorf("resources block wrong.\nwant:\n%s", want)
	}
}

func TestPortsAndDataMount(t *testing.T) {
	files := render(t, world())
	dep := string(files["deployment.yaml"])
	svc := string(files["service.yaml"])

	for _, want := range []string{"containerPort: 27015", "protocol: UDP", "protocol: TCP"} {
		if !strings.Contains(dep, want) {
			t.Errorf("deployment missing %q", want)
		}
	}
	if !strings.Contains(dep, "mountPath: "+DataMountPath) {
		t.Errorf("deployment does not mount the data volume at %s", DataMountPath)
	}
	if !strings.Contains(svc, "externalTrafficPolicy: Cluster") {
		t.Error("service must use externalTrafficPolicy Cluster on the gameserver node")
	}
	if !strings.Contains(svc, "192.168.20.225") {
		t.Error("service does not carry the instance LB IP")
	}
}

func TestSlotCarriesTheCollectionAndPassword(t *testing.T) {
	slot := string(render(t, world())["slot.yaml"])

	for _, want := range []string{
		`NAME: "TTT Night"`,
		`GAMEMODE: "terrortown"`,
		`MAP: "ttt_minecraft_b5"`,
		`MAXPLAYERS: "24"`,
		`PRODUCTION: "1"`,
	} {
		if !strings.Contains(slot, want) {
			t.Errorf("slot missing %s", want)
		}
	}
	if !strings.Contains(slot, "+host_workshop_collection 104604903") {
		t.Error("slot does not pass the Workshop collection through ARGS")
	}
	if !strings.Contains(slot, "+sv_password hunter2") {
		t.Error("slot does not pass the server password through ARGS")
	}
}

func TestSteamKeyComesFromASecretNotTheConfigMap(t *testing.T) {
	files := render(t, world())

	if strings.Contains(string(files["slot.yaml"]), "AUTHKEY") {
		t.Error("the shared Steam Web API key must never be rendered into a ConfigMap in git")
	}
	dep := string(files["deployment.yaml"])
	if !strings.Contains(dep, "name: AUTHKEY") || !strings.Contains(dep, "secretKeyRef") {
		t.Error("deployment does not read AUTHKEY from a Secret")
	}
}

func TestNoPrivateRegistryPullSecret(t *testing.T) {
	dep := string(render(t, world())["deployment.yaml"])

	if strings.Contains(dep, "imagePullSecrets") {
		t.Error("the gmod pod runs only a public image; a pull secret would name a Secret the gmod namespace never has")
	}
}

func TestStoppedWorldScalesToZero(t *testing.T) {
	inst := world()
	inst.State = domain.StateStopped

	if !strings.Contains(string(render(t, inst)["deployment.yaml"]), "replicas: 0") {
		t.Error("a stopped world must render replicas: 0")
	}
}
