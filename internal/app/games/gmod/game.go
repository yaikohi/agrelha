package gmod

import (
	"context"
	"fmt"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

const (
	DefaultImage         = "ceifa/garrysmod@sha256:51ca6117b29e9b9b4312d880da3bf16a058ab518ecf4449329cc57a37d58a30a"
	DefaultDataMountPath = "/home/gmod/server/garrysmod/data"
)

type Game struct {
	image          string
	runtime        ports.Runtime
	serverRef      ports.ServerRef
	statusProvider func(context.Context) (domain.GameTelemetry, error)
}

type Option func(*Game)

func WithImage(img string) Option {
	return func(g *Game) {
		if img != "" {
			g.image = img
		}
	}
}

func WithRuntime(rt ports.Runtime, ref ports.ServerRef) Option {
	return func(g *Game) {
		g.runtime = rt
		g.serverRef = ref
	}
}

func WithStatusProvider(fn func(context.Context) (domain.GameTelemetry, error)) Option {
	return func(g *Game) {
		g.statusProvider = fn
	}
}

func New(opts ...Option) *Game {
	g := &Game{
		image: DefaultImage,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

var _ ports.Game = (*Game)(nil)

func (g *Game) RuntimeSpec(inst domain.Instance) domain.RuntimeSpec {
	inst.GameID = domain.GameGMod
	return domain.RuntimeSpec{
		Image: g.image,
		Ports: []domain.PortSpec{
			{Name: "game", Port: 27015, Protocol: "UDP"},
			{Name: "rcon", Port: 27015, Protocol: "TCP"},
		},
		Volumes: []domain.VolumeSpec{
			{Name: "data", MountPath: DefaultDataMountPath, ReadOnly: false},
		},
		Env:         inst.Env(domain.GModProfile),
		HealthProbe: "/home/gmod/health.sh",
	}
}

func (g *Game) Telemetry(ctx context.Context) (domain.GameTelemetry, error) {
	if g.statusProvider != nil {
		return g.statusProvider(ctx)
	}

	tele := domain.GameTelemetry{
		State:        "unknown",
		Online:       false,
		CPU:          "—",
		Memory:       "—",
		Uptime:       "—",
		PlayersKnown: false,
	}

	if g.runtime != nil {
		if st, err := g.runtime.Status(ctx, g.serverRef); err == nil {
			if st.Available {
				tele.State = "Up"
				tele.Online = true
			} else if st.Lifecycle == ports.LifecycleStopped {
				tele.State = "Stopped"
				tele.Online = false
			} else if st.Lifecycle != "" {
				tele.State = string(st.Lifecycle)
			}
			if !st.StartedAt.IsZero() {
				tele.StartedAt = st.StartedAt
				tele.Uptime = domain.FormatDuration(time.Since(st.StartedAt))
			}
		}
		if m, err := g.runtime.Metrics(ctx, g.serverRef); err == nil && m.Known {
			tele.CPU = fmt.Sprintf("%dm", m.CPUMillicores)
			tele.Memory = fmt.Sprintf("%d Mi", m.MemoryMiB)
		}
	}

	return tele, nil
}

func (g *Game) ExportClientBundle(ctx context.Context, inst domain.Instance) (domain.Bundle, error) {
	return domain.Bundle{}, nil
}
