package manifests

import (
	"strings"
	"testing"

	"agrelha/internal/domain"
)

func renderMCDeployment(t *testing.T, inst domain.Instance) string {
	t.Helper()
	files, err := New("dedicated=gameserver", "minecraft-modded").Render(inst, domain.ModList{})
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	return string(files["deployment.yaml"])
}

// As with Valheim: an instance carrying no stored resources must render exactly
// what it rendered before resources became per-instance data.
func TestMinecraftLegacyInstanceRendersHistoricalResources(t *testing.T) {
	inst := domain.Instance{
		GameID: domain.GameMinecraft, Number: 1, Name: "World", Slug: "world",
		Tier: domain.TierMedium, State: domain.StateRunning,
	}
	want := strings.Join([]string{
		"          resources:",
		"            requests:",
		"              cpu: 2000m",
		"              memory: 8Gi",
		"            limits:",
		"              memory: 10Gi",
	}, "\n")

	if got := renderMCDeployment(t, inst); !strings.Contains(got, want) {
		t.Errorf("legacy resources block changed.\nwant:\n%s", want)
	}
}

// Heap-init is a Minecraft-only concern. It must follow the instance when set
// explicitly, and fall back to the historical per-tier value otherwise.
func TestMinecraftHeapInitFollowsInstance(t *testing.T) {
	legacy := domain.Instance{
		GameID: domain.GameMinecraft, Number: 1, Name: "World", Slug: "world",
		Tier: domain.TierLarge, State: domain.StateRunning,
	}
	if got := renderMCDeployment(t, legacy); !strings.Contains(got, "10G") {
		t.Error("large tier must keep its historical 10G heap init")
	}

	explicit := legacy
	explicit.Minecraft = &domain.MinecraftConfig{HeapInitGiB: 5}
	if got := renderMCDeployment(t, explicit); !strings.Contains(got, "5G") {
		t.Error("explicit heap init not rendered")
	}
}
