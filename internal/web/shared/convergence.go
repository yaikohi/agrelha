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
