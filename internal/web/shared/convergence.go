package shared

import (
	"fmt"

	"agrelha/internal/domain"
)

func convergenceNote(host string, unreachable bool) string {
	h := domain.NormalizeHost(host)
	switch {
	case unreachable:
		return fmt.Sprintf("this world restarts when %s is reachable again", h)
	case h.IsCluster():
		return "this world restarts once ArgoCD syncs"
	default:
		return "this world is restarting now"
	}
}

func ConvergenceClause(host string, unreachable bool) string {
	return " — " + convergenceNote(host, unreachable) + "."
}

func CreationSentence(host string, unreachable bool) string {
	h := domain.NormalizeHost(host)
	switch {
	case unreachable:
		return fmt.Sprintf(" It starts when %s is reachable again.", h)
	case h.IsCluster():
		return " ArgoCD will sync manifests."
	default:
		return " It is starting now."
	}
}

func ProvisioningStepCommitted(host string) string {
	return "Desired state committed to Codeberg repository"
}

func ProvisioningStepReconciling(host string) string {
	h := domain.NormalizeHost(host)
	if h.IsCluster() {
		return "ArgoCD reconciler syncing cluster state"
	}
	return fmt.Sprintf("Compose reconciler applying on %s", h)
}

func ProvisioningStepStarting(host string) string {
	h := domain.NormalizeHost(host)
	if h.IsCluster() {
		return "Pod scheduled and booting"
	}
	return fmt.Sprintf("Container starting on %s", h)
}

func ProvisioningDetail(host string) string {
	h := domain.NormalizeHost(host)
	if h.IsCluster() {
		return "Waiting for ArgoCD synchronization..."
	}
	return fmt.Sprintf("Applying compose on %s...", h)
}

func LogSourceNote(host string) string {
	if domain.NormalizeHost(host).IsCluster() {
		return "Real-time pod log stream"
	}
	return fmt.Sprintf("Real-time container log stream from %s", domain.NormalizeHost(host))
}
