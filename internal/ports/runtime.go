// Package ports declares the interfaces the domain needs from the outside
// world. Nothing here may import an adapter, and nothing here may name a
// technology: no Deployments, no replicas, no ConfigMaps, no containers.
//
// See docs/modularization-plan.md. Every port is designed against BOTH the
// Kubernetes and Docker adapters — a port validated by a single implementation
// is not a port, it is that implementation with extra steps.
package ports

import (
	"agrelha/internal/domain"
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotImplemented is returned by adapters that do not yet support an
// operation. It exists so a skeleton adapter can satisfy a port honestly
// instead of pretending to work.
var ErrNotImplemented = errors.New("not implemented by this runtime")

// ServerRef identifies one game server to a runtime without saying how it is
// implemented. Kubernetes resolves it to a Deployment, Docker to a container.
type ServerRef struct {
	// Name is the stable identifier, e.g. "mc-ayyy-01".
	Name string
	// Scope is the namespace (Kubernetes) or project (Compose). May be empty.
	Scope string
}

// Lifecycle is what the operator has asked the server to be. It says nothing
// about whether players can connect — see Status.Available and CONTEXT.md.
type Lifecycle string

const (
	LifecycleRunning Lifecycle = "running"
	LifecycleStopped Lifecycle = "stopped"
	LifecycleUnknown Lifecycle = "unknown"
)

// Status deliberately separates Lifecycle from Available. A server can be
// Lifecycle running and still unavailable while it installs mods or generates
// its world. Collapsing these two into one field is what once made a healthy
// Valheim server report "Offline".
type Status struct {
	Lifecycle Lifecycle
	Available bool
	StartedAt time.Time

	// Address is where players actually connect, as the runtime reports it.
	// Empty means the runtime has not assigned one yet — which must be shown as
	// such, not as the address the operator hoped for.
	Address string

	// Failure carries what the runtime knows about the server dying. It is
	// independent of Lifecycle (what the operator asked for) and of Available
	// (whether players can connect now).
	Failure Failure
}

// Failure is the runtime's account of the last time a server died. A runtime
// that cannot report this leaves it zero.
type Failure struct {
	RestartCount  int32
	WaitingReason string

	ExitCode   int32
	Reason     string
	FinishedAt time.Time
	OOMKilled  bool

	// InitRestartCount and InitStep describe a failure that happened before the
	// server started at all - a mod install, say. Kept apart from RestartCount
	// so "the game crashed" and "setup failed" stay distinguishable.
	InitRestartCount int32
	InitStep         string
}

// Crashed reports whether the runtime has seen this server die.
func (f Failure) Crashed() bool {
	return f.RestartCount > 0 || f.InitRestartCount > 0 ||
		f.WaitingReason == "CrashLoopBackOff" || !f.FinishedAt.IsZero()
}

// FailedBeforeStart reports whether the server never ran because a setup step
// failed. Such a failure needs a different fix from a server that crashed.
func (f Failure) FailedBeforeStart() bool { return f.InitRestartCount > 0 || f.InitStep != "" }

// Metrics is current resource usage. Zero values mean "unknown", not "idle" —
// not every runtime can report this.
type Metrics struct {
	CPUMillicores int64
	MemoryMiB     int64
	Known         bool
}

// LogOptions selects which log lines to return.
type LogOptions struct {
	// Tail is the number of trailing lines; 0 means the runtime's default.
	Tail int64
	// Follow streams new lines until the context is cancelled.
	Follow bool
	// Container selects a specific container by name. Empty picks the game
	// server. A setup step's logs live in its own container, and the server
	// container has none when that step never let it start.
	Container string
	// Previous reads the logs of the previous, terminated container instead of
	// the running one. This is the only way to see why a server crashed, and
	// the runtime discards it once the container is reaped.
	Previous bool
}

// Runtime makes game servers run. Implementations: adapters/runtime/k8s and
// adapters/runtime/docker.
//
// Deliberately absent: anything about volumes, images or scheduling. Those are
// how a server is *defined*, which is the StateStore's concern, not the
// runtime's. One-shot tasks (backup/restore) are not here yet because their
// only implementation is expressed in PVC claim names; generalising that needs
// the volume model from a later phase.
type Runtime interface {
	Start(ctx context.Context, ref ServerRef) error
	Stop(ctx context.Context, ref ServerRef) error
	Restart(ctx context.Context, ref ServerRef) error
	Status(ctx context.Context, ref ServerRef) (Status, error)
	Metrics(ctx context.Context, ref ServerRef) (Metrics, error)
	Logs(ctx context.Context, ref ServerRef, opts LogOptions) (io.ReadCloser, error)
	WatchAvailability(ctx context.Context, ref ServerRef, timeout time.Duration) error
}

type Console interface {
	Execute(cmd string) (string, error)
}

type SpecRenderer interface {
	Render(inst domain.Instance, modsTxt string) (map[string][]byte, error)
}

// JobRunner creates one-shot batch tasks such as backups and restores.
type JobRunner interface {
	CreateBackupJob(ctx context.Context, jobName, archiveName, sourcePVC, backupPVC string) error
	CreateRestoreJob(ctx context.Context, jobName, archiveName, targetPVC, backupPVC string) error
}
