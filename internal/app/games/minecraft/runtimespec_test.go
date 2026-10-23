package minecraft

import (
	"strings"
	"testing"

	"agrelha/internal/domain"
)

func modpackWorld() domain.Instance {
	return domain.Instance{
		GameID: domain.GameMinecraft, Number: 1, Name: "Help", Slug: "help",
		Tier: domain.TierLarge, Host: "fogo", Source: domain.SourceModpack,
		Minecraft: &domain.MinecraftConfig{
			MCVersion: "1.21.1",
			Pack:      &domain.Pack{Provider: domain.ProviderCurseForge, Ref: "https://curseforge.com/x", Name: "P"},
		},
	}
}

func TestTheRuntimeSpecCarriesTheEulaAcceptance(t *testing.T) {
	spec := New().RuntimeSpec(modpackWorld())
	if spec.Env["EULA"] != "TRUE" {
		t.Errorf(`RuntimeSpec carries no EULA acceptance (env: %v).

On the cluster EULA is hardcoded in the Deployment template, so Instance.Env never needed it. A
Compose world is built from RuntimeSpec alone, and itzg refuses to boot without it - the container
crash-loops printing "Please accept the Minecraft EULA" and the world never starts.`, spec.Env)
	}
}

func TestTheRuntimeSpecCarriesTheChosenResourceTier(t *testing.T) {
	spec := New().RuntimeSpec(modpackWorld())
	if spec.Env["MAX_MEMORY"] == "" || spec.Env["INIT_MEMORY"] == "" {
		t.Fatalf(`RuntimeSpec carries no heap sizing (env: %v).

The operator picks a RAM tier at creation; without MAX_MEMORY the JVM silently takes its own default
and the tier is a lie.`, spec.Env)
	}
	if !strings.HasSuffix(spec.Env["MAX_MEMORY"], "G") {
		t.Errorf("MAX_MEMORY %q is not a JVM heap size", spec.Env["MAX_MEMORY"])
	}
}

func TestTheRuntimeSpecEnablesRconOnlyWithAPassword(t *testing.T) {
	withPass := New(WithRconPassword("s3cret")).RuntimeSpec(modpackWorld())
	if withPass.Env["ENABLE_RCON"] != "TRUE" || withPass.Env["RCON_PASSWORD"] != "s3cret" {
		t.Errorf("RCON was not configured despite a password being supplied: %v", withPass.Env)
	}

	without := New().RuntimeSpec(modpackWorld())
	if _, ok := without.Env["RCON_PASSWORD"]; ok {
		t.Error("an empty password must not reach the server as an empty RCON_PASSWORD, which would open the console to anyone on the host")
	}
}

func TestWorldShapedEnvStillWins(t *testing.T) {
	spec := New().RuntimeSpec(modpackWorld())
	if spec.Env["TYPE"] != "AUTO_CURSEFORGE" {
		t.Errorf(`the world's own TYPE was overwritten by operational defaults; got %q.

Instance.Env decides what the world *is*; RuntimeSpec only fills in how it runs. Operational
defaults must never shadow it.`, spec.Env["TYPE"])
	}
}
