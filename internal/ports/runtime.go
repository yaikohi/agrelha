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
}

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
