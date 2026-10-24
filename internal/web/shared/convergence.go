package shared

import (
	"fmt"

	"agrelha/internal/domain"
)

type ConvergenceMode int

const (
	ConvergenceDirect ConvergenceMode = iota
	ConvergenceCluster
	ConvergenceUnreachable
)

type HostTarget struct {
	Host        domain.Host
	Unreachable bool
}

func Target(host domain.Host, unreachable bool) HostTarget {
	return HostTarget{
		Host:        domain.NormalizeHost(string(host)),
		Unreachable: unreachable,
	}
}

func TargetFromInstance(inst domain.Instance) HostTarget {
	return Target(inst.EffectiveHost(), inst.HostUnreachable)
}

func (t HostTarget) Mode() ConvergenceMode {
	if t.Unreachable {
		return ConvergenceUnreachable
	}
	if t.Host.IsCluster() {
		return ConvergenceCluster
	}
	return ConvergenceDirect
}

func (t HostTarget) ConvergenceClause() string {
	return ConvergenceClause(t.Host, t.Unreachable)
}

func (t HostTarget) CreationSentence() string {
	return CreationSentence(t.Host, t.Unreachable)
}

func convergenceNote(t HostTarget) string {
	switch t.Mode() {
	case ConvergenceUnreachable:
		return fmt.Sprintf("this world restarts when %s is reachable again", t.Host)
	case ConvergenceCluster:
		return "this world restarts once ArgoCD syncs"
	default:
		return "this world is restarting now"
	}
}

func ConvergenceClause(host domain.Host, unreachable bool) string {
	return " — " + convergenceNote(Target(host, unreachable)) + "."
}

func CreationSentence(host domain.Host, unreachable bool) string {
	t := Target(host, unreachable)
	switch t.Mode() {
	case ConvergenceUnreachable:
		return fmt.Sprintf(" It starts when %s is reachable again.", t.Host)
	case ConvergenceCluster:
		return " ArgoCD will sync manifests."
	default:
		return " It is starting now."
	}
}

func ProvisioningStepCommitted(host domain.Host) string {
	return "Desired state committed to Codeberg repository"
}

func ProvisioningStepReconciling(host domain.Host) string {
	h := domain.NormalizeHost(string(host))
	if h.IsCluster() {
		return "ArgoCD reconciler syncing cluster state"
	}
	return fmt.Sprintf("Compose reconciler applying on %s", h)
}

func ProvisioningStepStarting(host domain.Host) string {
	h := domain.NormalizeHost(string(host))
	if h.IsCluster() {
		return "Pod scheduled and booting"
	}
	return fmt.Sprintf("Container starting on %s", h)
}

func ProvisioningDetail(host domain.Host) string {
	h := domain.NormalizeHost(string(host))
	if h.IsCluster() {
		return "Waiting for ArgoCD synchronization..."
	}
	return fmt.Sprintf("Applying compose on %s...", h)
}

func LogSourceNote(host domain.Host) string {
	h := domain.NormalizeHost(string(host))
	if h.IsCluster() {
		return "Real-time pod log stream"
	}
	return fmt.Sprintf("Real-time container log stream from %s", h)
}
