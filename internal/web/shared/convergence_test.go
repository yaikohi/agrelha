package shared

import (
	"strings"
	"testing"

	"agrelha/internal/domain"
)

func TestConvergenceClauseByHost(t *testing.T) {
	tests := []struct {
		name        string
		host        domain.Host
		unreachable bool
		want        string
	}{
		{"cluster", "cluster", false, " — this world restarts once ArgoCD syncs."},
		{"empty host is the cluster", "", false, " — this world restarts once ArgoCD syncs."},
		{"docker host converges synchronously", "fogo", false, " — this world is restarting now."},
		{"unreachable names the host", "fogo", true, " — this world restarts when fogo is reachable again."},
		{"unreachable wins over cluster", "cluster", true, " — this world restarts when cluster is reachable again."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ConvergenceClause(tc.host, tc.unreachable); got != tc.want {
				t.Errorf("ConvergenceClause(%q, %v)\n got %q\nwant %q", tc.host, tc.unreachable, got, tc.want)
			}
		})
	}
}

func TestADockerHostIsNeverToldAboutArgoCD(t *testing.T) {
	for _, host := range []domain.Host{"fogo", "FOGO", " fogo "} {
		if strings.Contains(ConvergenceClause(host, false), "ArgoCD") {
			t.Errorf("host %q was promised an ArgoCD sync that will never happen", host)
		}
	}
}

func TestANewWorldIsNotDescribedAsRestarting(t *testing.T) {
	for _, host := range []domain.Host{"cluster", "fogo"} {
		got := CreationSentence(host, false)
		if strings.Contains(got, "restart") {
			t.Errorf("CreationSentence(%q) = %q; a world being created for the first time has nothing to restart", host, got)
		}
		if !strings.HasPrefix(got, " ") || got[1:2] != strings.ToUpper(got[1:2]) {
			t.Errorf("CreationSentence(%q) = %q; it follows a full stop, so it must be a capitalised sentence", host, got)
		}
	}
	if got := CreationSentence("cluster", false); got != " ArgoCD will sync manifests." {
		t.Errorf("cluster creation copy changed: %q", got)
	}
	if got := CreationSentence("fogo", true); got != " It starts when fogo is reachable again." {
		t.Errorf("unreachable creation copy = %q", got)
	}
}

func TestHostTargetConvergenceMode(t *testing.T) {
	if m := Target("cluster", false).Mode(); m != ConvergenceCluster {
		t.Fatalf("Target('cluster', false).Mode() = %v, want ConvergenceCluster; cluster world will not wait for ArgoCD sync", m)
	}
	if m := Target("fogo", false).Mode(); m != ConvergenceDirect {
		t.Fatalf("Target('fogo', false).Mode() = %v, want ConvergenceDirect; direct docker world will wait for non-existent ArgoCD sync", m)
	}
	if m := Target("cluster", true).Mode(); m != ConvergenceUnreachable {
		t.Fatalf("Target('cluster', true).Mode() = %v, want ConvergenceUnreachable; unreachable host will promise immediate restart", m)
	}
	if m := Target("fogo", true).Mode(); m != ConvergenceUnreachable {
		t.Fatalf("Target('fogo', true).Mode() = %v, want ConvergenceUnreachable; unreachable host will promise immediate restart", m)
	}
}

func TestTargetFromInstance(t *testing.T) {
	inst := domain.Instance{
		Host:            "fogo",
		HostUnreachable: true,
	}
	tgt := TargetFromInstance(inst)
	if tgt.Host != "fogo" || !tgt.Unreachable {
		t.Fatalf("TargetFromInstance(%+v) = %+v, want Host='fogo' and Unreachable=true; convergence copy will mismatch instance state", inst, tgt)
	}
	if tgt.Mode() != ConvergenceUnreachable {
		t.Fatalf("tgt.Mode() = %v, want ConvergenceUnreachable; unreachable world will report incorrect restart clause", tgt.Mode())
	}
	if got := tgt.ConvergenceClause(); got != " — this world restarts when fogo is reachable again." {
		t.Fatalf("tgt.ConvergenceClause() = %q, want unreachable restart clause", got)
	}
}
