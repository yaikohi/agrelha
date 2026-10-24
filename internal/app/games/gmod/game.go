package gmod

import (
	"context"

	"agrelha/internal/app/games"
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

	return games.CollectTelemetry(ctx, g.runtime, g.serverRef), nil
}

func (g *Game) ExportClientBundle(ctx context.Context, inst domain.Instance) (domain.Bundle, error) {
	return domain.Bundle{}, ports.ErrNotImplemented
}
