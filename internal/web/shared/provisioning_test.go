package shared

import (
	"strings"
	"testing"

	"agrelha/internal/domain"
)

// Words that describe how agrelha builds a world rather than what is happening
// to it. "fogo" is the Docker Host's name: the person waiting for their server
// has no idea which machine that is, and it is not something they can act on.
var plumbing = []string{
	"ArgoCD", "Pod", "pod", "container", "Container",
	"compose", "Compose", "Codeberg", "reconciler", "Reconciler",
	"manifest", "Desired state", "fogo", "cluster",
}

func assertNoPlumbing(t *testing.T, label, copy string) {
	t.Helper()
	for _, word := range plumbing {
		if strings.Contains(copy, word) {
			t.Errorf("%s reads %q, which names %q - that is agrelha's plumbing, not something a player can act on", label, copy, word)
		}
	}
}

// The progress page is read by whoever asked for the world, not by whoever
// runs the host it lands on.
func TestProvisioningCopyNamesNoInfrastructure(t *testing.T) {
	for label, copy := range map[string]string{
		"ProvisioningStepCommitted":   ProvisioningStepCommitted,
		"ProvisioningStepReconciling": ProvisioningStepReconciling,
		"ProvisioningStepStarting":    ProvisioningStepStarting,
		"ProvisioningDetail":          ProvisioningDetail,
		"LogSourceNote":               LogSourceNote,
	} {
		assertNoPlumbing(t, label, copy)
		if strings.TrimSpace(copy) == "" {
			t.Errorf("%s is empty", label)
		}
	}
}

// Whether a change has already happened or is about to is something a player
// observes, so this wording still varies - it just no longer explains why.
func TestConvergenceWordingStillDistinguishesWhen(t *testing.T) {
	direct := ConvergenceClause("fogo", false)
	cluster := ConvergenceClause(domain.HostCluster, false)
	unreachable := ConvergenceClause("fogo", true)

	if direct == cluster {
		t.Error("an immediate restart and a deferred one read identically")
	}
	if unreachable == direct || unreachable == cluster {
		t.Error("an unreachable host must not promise a restart like a reachable one")
	}
	for label, copy := range map[string]string{
		"direct":      direct,
		"cluster":     cluster,
		"unreachable": unreachable,
		"created":     CreationSentence("fogo", false),
		"created/far": CreationSentence(domain.HostCluster, false),
		"created/off": CreationSentence("fogo", true),
	} {
		assertNoPlumbing(t, "ConvergenceClause "+label, copy)
	}
}

// An unset Host reads as cluster everywhere else, and must here too.
func TestUnsetHostMatchesCluster(t *testing.T) {
	if ConvergenceClause("", false) != ConvergenceClause(domain.HostCluster, false) {
		t.Error("an unset Host must read as cluster, matching domain.NormalizeHost")
	}
}
