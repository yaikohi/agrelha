package minecraft

import (
	"context"
	"fmt"

	"agrelha/internal/domain"
	"agrelha/internal/modpack"
	"agrelha/internal/ports"
)

const (
	DefaultImage = "itzg/minecraft-server:java25"
)

// Game implements ports.Game for Minecraft.
type Game struct {
	providers []ports.ContentProvider
	image     string
}

// Option configures a Minecraft Game instance.
type Option func(*Game)

// WithProvider registers a content provider (e.g. Modrinth, CurseForge).
func WithProvider(p ports.ContentProvider) Option {
	return func(g *Game) {
		g.providers = append(g.providers, p)
	}
}

// WithImage overrides the container image.
func WithImage(img string) Option {
	return func(g *Game) {
		if img != "" {
			g.image = img
		}
	}
}

// New creates a new Minecraft game engine instance.
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

func (g *Game) ID() domain.GameID {
	return domain.GameMinecraft
}

func (g *Game) Display() domain.Display {
	return domain.Display{
		Name:   "Minecraft",
		Icon:   "pickaxe",
		Accent: "emerald",
	}
}

func (g *Game) AdmissionModel() domain.AdmissionModel {
	return domain.AdmissionAllowlist
}

func (g *Game) OperatorIDKind() domain.OperatorIDKind {
	return domain.IDKindUsername
}

func (g *Game) Providers() []ports.ContentProvider {
	return g.providers
}

// RuntimeSpec defines the execution shape of a Minecraft server container.
func (g *Game) RuntimeSpec(inst domain.Instance) domain.RuntimeSpec {
	return domain.RuntimeSpec{
		Image: g.image,
		Ports: []domain.PortSpec{
			{Name: "minecraft", Port: 25565, Protocol: "TCP"},
			{Name: "rcon", Port: 25575, Protocol: "TCP"},
		},
		Volumes: []domain.VolumeSpec{
			{Name: "data", MountPath: "/data", ReadOnly: false},
		},
		Env:         inst.Env(),
		HealthProbe: "mc-health",
	}
}

func (g *Game) ResolveContent(ctx context.Context, inst domain.Instance) (domain.ContentSet, error) {
	return domain.ContentSet{}, nil
}

// ExportClientBundle creates an .mrpack Modrinth bundle for the instance.
func (g *Game) ExportClientBundle(ctx context.Context, inst domain.Instance) (domain.Bundle, error) {
	loader := string(inst.Loader)
	if loader == "" {
		loader = string(domain.LoaderNeoForge)
	}
	mcVer := inst.MCVersion
	if mcVer == "" {
		mcVer = "1.21.1"
	}

	data, err := modpack.BuildMrpack(ctx, nil, inst.Name, mcVer, loader, "latest", nil, nil)
	if err != nil {
		return domain.Bundle{}, fmt.Errorf("build minecraft client bundle: %w", err)
	}

	return domain.Bundle{
		Filename:    fmt.Sprintf("%s.mrpack", inst.Slug),
		ContentType: "application/x-modrinth-modpack+zip",
		Data:        data,
	}, nil
}
