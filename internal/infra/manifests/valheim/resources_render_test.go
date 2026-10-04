package valheim

import (
	"strings"
	"testing"

	"agrelha/internal/domain"
)

func renderDeployment(t *testing.T, inst domain.Instance) string {
	t.Helper()
	files, err := New("dedicated=gameserver", "valheim").Render(inst, domain.ModList{})
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	return string(files["deployment.yaml"])
}

// An instance with no stored resources must render exactly what it rendered
// before resources became per-instance data: 2000m cpu, the tier's memory, and
// no cpu limit. If this block ever changes shape, every world restarts.
func TestLegacyInstanceRendersHistoricalResources(t *testing.T) {
	inst := domain.Instance{
		GameID: domain.GameValheim, Number: 1, Name: "Odin", Slug: "odin",
		Tier: domain.TierMedium, State: domain.StateRunning,
	}
	want := strings.Join([]string{
		"          resources:",
		"            requests:",
		"              cpu: 2000m",
		"              memory: 6Gi",
		"            limits:",
		"              memory: 7Gi",
	}, "\n")

	if got := renderDeployment(t, inst); !strings.Contains(got, want) {
		t.Errorf("legacy resources block changed.\nwant:\n%s", want)
	}
}

func TestExplicitResourcesOverrideTier(t *testing.T) {
	inst := domain.Instance{
		GameID: domain.GameValheim, Number: 1, Name: "Odin", Slug: "odin",
		Tier: domain.TierMedium, State: domain.StateRunning,
		Resources: domain.Resources{
			MemRequestGiB: 16, MemLimitGiB: 20,
			CPURequestMilli: 4000, CPULimitMilli: 8000,
		},
	}
	got := renderDeployment(t, inst)

	for _, want := range []string{"cpu: 4000m", "memory: 16Gi", "cpu: 8000m", "memory: 20Gi"} {
		if !strings.Contains(got, want) {
			t.Errorf("explicit resources not rendered: missing %q", want)
		}
	}
	if strings.Contains(got, "memory: 6Gi") {
		t.Error("tier memory leaked into a manifest with explicit resources")
	}
}
