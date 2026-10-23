package shared

import (
	"strings"
	"testing"
)

func TestProvisioningCopyFollowsTheHost(t *testing.T) {
	for _, step := range []func(string) string{
		ProvisioningStepReconciling,
		ProvisioningStepStarting,
		ProvisioningDetail,
	} {
		onCluster := step("cluster")
		onDocker := step("fogo")

		if onCluster == onDocker {
			t.Errorf("provisioning copy %q is identical on a cluster and on a Docker Host", onCluster)
		}
		if strings.Contains(onDocker, "ArgoCD") || strings.Contains(onDocker, "Pod") {
			t.Errorf(`a Docker-Host world was told %q.

There is no ArgoCD and no Pod on a Docker Host, so the progress page narrates a sequence that will
never happen - and the operator watches for a sync that nobody is going to perform.`, onDocker)
		}
		if !strings.Contains(onDocker, "fogo") {
			t.Errorf("Docker-Host copy %q never names the Host it is waiting on", onDocker)
		}
	}
}

func TestProvisioningCopyKeepsTheClusterWordingItAlwaysHad(t *testing.T) {
	if !strings.Contains(ProvisioningStepReconciling("cluster"), "ArgoCD") {
		t.Error("a cluster world must still be told ArgoCD is syncing; that is what actually happens there")
	}
	if !strings.Contains(ProvisioningStepStarting("cluster"), "Pod") {
		t.Error("a cluster world must still be told a Pod is scheduling")
	}
	if ProvisioningStepReconciling("") != ProvisioningStepReconciling("cluster") {
		t.Error("an unset Host must read as cluster, matching domain.NormalizeHost")
	}
}

func TestTheConsoleNamesWhereItsLogsComeFrom(t *testing.T) {
	onCluster := LogSourceNote("cluster")
	onDocker := LogSourceNote("fogo")

	if strings.Contains(onDocker, "pod") {
		t.Errorf(`a Docker-Host console says %q.

There is no pod on a Docker Host; the lines come from a container through the runtime. Calling it a
pod stream is the same cluster assumption that left this page empty for every Docker-Host world.`, onDocker)
	}
	if !strings.Contains(onDocker, "fogo") {
		t.Errorf("Docker-Host console copy %q never names the Host", onDocker)
	}
	if !strings.Contains(onCluster, "pod") {
		t.Error("a cluster console must still say pod; that is what it reads")
	}
}
