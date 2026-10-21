package shared

import (
	"strings"
	"testing"
)

func TestConvergenceClauseByHost(t *testing.T) {
	tests := []struct {
		name        string
		host        string
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
	for _, host := range []string{"fogo", "FOGO", " fogo "} {
		if strings.Contains(ConvergenceClause(host, false), "ArgoCD") {
			t.Errorf("host %q was promised an ArgoCD sync that will never happen", host)
		}
	}
}

func TestANewWorldIsNotDescribedAsRestarting(t *testing.T) {
	for _, host := range []string{"cluster", "fogo"} {
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
