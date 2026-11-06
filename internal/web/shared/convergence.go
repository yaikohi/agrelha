package shared

import (
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
		return "this world restarts once the machine it runs on is reachable again"
	case ConvergenceCluster:
		return "this world restarts in a moment"
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
		return " It starts once the machine it runs on is reachable again."
	case ConvergenceCluster:
		return " It starts in a moment."
	default:
		return " It is starting now."
	}
}

// These describe what is happening to the world, not how agrelha builds it.
// "fogo", "compose", "Codeberg", "ArgoCD", "pod" and "container" are plumbing:
// the person waiting for their server has no idea which machine fogo is, and
// naming it tells them nothing they can act on. The mode-dependent wording
// above still varies, because *when* a change takes effect is something a
// player can actually observe.
const (
	ProvisioningStepCommitted   = "World settings saved"
	ProvisioningStepReconciling = "Preparing the server"
	ProvisioningStepStarting    = "Starting the server"
	ProvisioningDetail          = "Setting up your world…"
	LogSourceNote               = "Live server log"
)
