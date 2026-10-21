package valheim

import (
	"strings"
	"testing"

	"agrelha/internal/domain"
)

func moddedDeployment(t *testing.T) string {
	t.Helper()
	inst := domain.Instance{
		GameID: domain.GameValheim, Number: 1, Name: "Odin", Slug: "odin",
		Tier: domain.TierMedium, State: domain.StateRunning,
		Valheim: &domain.ValheimConfig{Password: "secret"},
	}
	files, err := New("ykhi.xyz/gameserver=true", "valheim").Render(inst, domain.ModList{Primary: "a-b-1.0.0\n"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return string(files["deployment.yaml"])
}

func TestExtractedScriptIsEmbeddedInTheDeployment(t *testing.T) {
	if strings.TrimSpace(modReconcileScript) == "" {
		t.Fatal("the mod reconciler script is empty: the embed did not find scripts/mod-reconcile.sh")
	}
	dep := moddedDeployment(t)
	for _, want := range []string{
		"              set -eu",
		"              MODS=/config-mods/mods.txt",
		"              chown -R 1000:1000 /config/bepinex 2>/dev/null || true",
	} {
		if !strings.Contains(dep, want) {
			t.Errorf("rendered deployment missing %q", want)
		}
	}
}

func TestScriptBlankLinesStayEmpty(t *testing.T) {
	dep := moddedDeployment(t)
	if strings.Contains(dep, "\n              \n") {
		t.Error("a blank line inside the script was padded with indentation; the original leaves them truly empty and padding them changes every rendered manifest")
	}
}

func TestScriptCarriesNoShebang(t *testing.T) {
	if strings.HasPrefix(modReconcileScript, "#!") {
		t.Error("a shebang would be rendered into the YAML block; the image runs this via sh -c, and the Nix packaging adds its own")
	}
}
