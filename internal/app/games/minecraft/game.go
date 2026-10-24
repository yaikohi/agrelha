package minecraft

import (
	"context"
	"fmt"

	"agrelha/internal/app/games"
	"agrelha/internal/app/modpack"
	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

const (
	DefaultImage = "itzg/minecraft-server:java25"
)

// Game implements ports.Game for Minecraft.
type Game struct {
	image            string
	rconPassword     string
	runtime          ports.Runtime
	serverRef        ports.ServerRef
	playerCountFn    func(context.Context) (int, error)
	activeInstanceFn func(context.Context) (domain.Loader, string)
	statusProvider   func(context.Context) (domain.GameTelemetry, error)
	bundleBuilder    func(context.Context, domain.Instance) (domain.Bundle, error)
}

// Option configures a Minecraft Game instance.
type Option func(*Game)

// WithImage overrides the container image.
func WithImage(img string) Option {
	return func(g *Game) {
		if img != "" {
			g.image = img
		}
	}
}

func WithRconPassword(pass string) Option {
	return func(g *Game) { g.rconPassword = pass }
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

// RuntimeSpec defines the execution shape of a Minecraft server container.
const jvmOpts = "-XX:+UseG1GC -XX:+ParallelRefProcEnabled -XX:MaxGCPauseMillis=200 -XX:+UnlockExperimentalVMOptions -XX:+DisableExplicitGC -XX:+AlwaysPreTouch"

func (g *Game) RuntimeSpec(inst domain.Instance) domain.RuntimeSpec {
	inst.GameID = domain.GameMinecraft
	env := inst.Env(domain.MinecraftProfile)
	if env == nil {
		env = map[string]string{}
	}
	operational := map[string]string{
		"EULA":              "TRUE",
		"INIT_MEMORY":       fmt.Sprintf("%dG", inst.HeapInitMemoryGiB(domain.MinecraftProfile)),
		"MAX_MEMORY":        fmt.Sprintf("%dG", inst.MemoryGiB(domain.MinecraftProfile)),
		"SYNC_CHUNK_WRITES": "FALSE",
		"JVM_XX_OPTS":       jvmOpts,
		"TZ":                "Europe/Amsterdam",
		"ENABLE_QUERY":      "TRUE",
		"ONLINE_MODE":       "TRUE",
		"ENABLE_RCON":       "TRUE",
		"RCON_PORT":         fmt.Sprintf("%d", domain.MinecraftRconPort),
	}
	if g.rconPassword != "" {
		operational["RCON_PASSWORD"] = g.rconPassword
	}
	for k, v := range operational {
		if _, set := env[k]; !set {
			env[k] = v
		}
	}
	return domain.RuntimeSpec{
		Image: g.image,
		Ports: []domain.PortSpec{
			{Name: "minecraft", Port: domain.MinecraftGamePort, Protocol: "TCP"},
			{Name: "rcon", Port: domain.MinecraftRconPort, Protocol: "TCP"},
		},
		Volumes: []domain.VolumeSpec{
			{Name: "data", MountPath: "/data", ReadOnly: false},
			{Name: "config-mods", MountPath: "/config-mods", ReadOnly: true},
			{Name: "custom-configs", MountPath: "/custom-configs", ReadOnly: true},
		},
		Env:         env,
		HealthProbe: "mc-health",
	}
}

func (g *Game) Telemetry(ctx context.Context) (domain.GameTelemetry, error) {
	if g.statusProvider != nil {
		return g.statusProvider(ctx)
	}

	tele := games.CollectTelemetry(ctx, g.runtime, g.serverRef)
	tele.Loader = "NeoForge"

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

	return tele, nil
}

var buildMrpack = modpack.BuildMrpack

// ExportClientBundle creates an .mrpack Modrinth bundle for the instance.
func (g *Game) ExportClientBundle(ctx context.Context, inst domain.Instance) (domain.Bundle, error) {
	if g.bundleBuilder != nil {
		return g.bundleBuilder(ctx, inst)
	}

	loader := ""
	mcVer := ""
	if inst.Minecraft != nil {
		loader = string(inst.Minecraft.Loader)
		mcVer = inst.Minecraft.MCVersion
	}
	if loader == "" {
		loader = string(domain.LoaderNeoForge)
	}
	if mcVer == "" {
		mcVer = "1.21.1"
	}

	data, err := buildMrpack(ctx, nil, inst.Name, mcVer, loader, "latest", nil, nil, nil)
	if err != nil {
		return domain.Bundle{}, fmt.Errorf("build minecraft client bundle: %w", err)
	}

	return domain.Bundle{
		Filename:    fmt.Sprintf("%s.mrpack", inst.Slug),
		ContentType: "application/x-modrinth-modpack+zip",
		Data:        data,
	}, nil
}
