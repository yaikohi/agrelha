package minecraft

import (
	"context"
	"fmt"
	"time"

	"agrelha/internal/app/modpack"
	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

const (
	DefaultImage = "itzg/minecraft-server:java25"
)

// Game implements ports.Game for Minecraft.
type Game struct {
	providers        []ports.ContentProvider
	image            string
	runtime          ports.Runtime
	serverRef        ports.ServerRef
	playerCountFn    func(context.Context) (int, error)
	activeInstanceFn func(context.Context) (domain.Loader, string)
	statusProvider   func(context.Context) (domain.GameTelemetry, error)
	contentResolver  func(context.Context, domain.Instance) (domain.ContentSet, error)
	bundleBuilder    func(context.Context, domain.Instance) (domain.Bundle, error)
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

// WithRuntime associates a workload execution runtime and server reference.
func WithRuntime(rt ports.Runtime, ref ports.ServerRef) Option {
	return func(g *Game) {
		g.runtime = rt
		g.serverRef = ref
	}
}

// WithPlayerCount registers a callback returning current online players.
func WithPlayerCount(fn func(context.Context) (int, error)) Option {
	return func(g *Game) {
		g.playerCountFn = fn
	}
}

// WithActiveInstance registers a callback returning the active instance loader and pack name.
func WithActiveInstance(fn func(context.Context) (domain.Loader, string)) Option {
	return func(g *Game) {
		g.activeInstanceFn = fn
	}
}

// WithStatusProvider overrides telemetry gathering with a custom provider.
func WithStatusProvider(fn func(context.Context) (domain.GameTelemetry, error)) Option {
	return func(g *Game) {
		g.statusProvider = fn
	}
}

// WithContentResolver sets the content resolution strategy.
func WithContentResolver(fn func(context.Context, domain.Instance) (domain.ContentSet, error)) Option {
	return func(g *Game) {
		g.contentResolver = fn
	}
}

// WithBundleBuilder sets a custom client bundle builder.
func WithBundleBuilder(fn func(context.Context, domain.Instance) (domain.Bundle, error)) Option {
	return func(g *Game) {
		g.bundleBuilder = fn
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
	inst.GameID = domain.GameMinecraft
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
		Loader:       "NeoForge",
		PlayersKnown: false,
	}

	if g.playerCountFn != nil {
		if n, err := g.playerCountFn(ctx); err == nil {
			tele.Players = n
			tele.PlayersKnown = true
		}
	}

	if g.activeInstanceFn != nil {
		loader, pack := g.activeInstanceFn(ctx)
		if loader == domain.LoaderFabric {
			tele.Loader = "Fabric"
		} else if loader != "" {
			tele.Loader = string(loader)
		}
		if pack != "" {
			tele.PackName = pack
		}
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

func (g *Game) ResolveContent(ctx context.Context, inst domain.Instance) (domain.ContentSet, error) {
	if g.contentResolver != nil {
		return g.contentResolver(ctx, inst)
	}
	return domain.ContentSet{}, nil
}

var buildMrpack = modpack.BuildMrpack

// ExportClientBundle creates an .mrpack Modrinth bundle for the instance.
func (g *Game) ExportClientBundle(ctx context.Context, inst domain.Instance) (domain.Bundle, error) {
	if g.bundleBuilder != nil {
		return g.bundleBuilder(ctx, inst)
	}

	loader := string(inst.Loader)
	if loader == "" {
		loader = string(domain.LoaderNeoForge)
	}
	mcVer := inst.MCVersion
	if mcVer == "" {
		mcVer = "1.21.1"
	}

	data, err := buildMrpack(ctx, nil, inst.Name, mcVer, loader, "latest", nil, nil)
	if err != nil {
		return domain.Bundle{}, fmt.Errorf("build minecraft client bundle: %w", err)
	}

	return domain.Bundle{
		Filename:    fmt.Sprintf("%s.mrpack", inst.Slug),
		ContentType: "application/x-modrinth-modpack+zip",
		Data:        data,
	}, nil
}
