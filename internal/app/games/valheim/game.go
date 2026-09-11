package valheim

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agrelha/internal/app/modpack"
	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

const (
	FallbackBepInExVersion = "5.4.2333"
	DefaultImage           = "lloesche/valheim-server:latest"
)

// Game implements ports.Game for Valheim.
type Game struct {
	providers       []ports.ContentProvider
	image           string
	runtime         ports.Runtime
	serverRef       ports.ServerRef
	playerCountFn   func() (int, error)
	statusProvider  func(context.Context) (domain.GameTelemetry, error)
	contentResolver func(context.Context, domain.Instance) (domain.ContentSet, error)
	bundleSource    func(context.Context, domain.Instance) (entries []string, configs map[string]string, err error)
}

// Option configures a Valheim Game instance.
type Option func(*Game)

// WithProvider registers a content provider (e.g. Thunderstore).
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
func WithPlayerCount(fn func() (int, error)) Option {
	return func(g *Game) {
		g.playerCountFn = fn
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

// WithBundleSource sets a provider for mod entries and config files when exporting client bundles.
func WithBundleSource(fn func(context.Context, domain.Instance) ([]string, map[string]string, error)) Option {
	return func(g *Game) {
		g.bundleSource = fn
	}
}

// New creates a new Valheim game engine instance.
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
	return domain.GameValheim
}

func (g *Game) Display() domain.Display {
	return domain.Display{
		Name:   "Valheim",
		Icon:   "axe",
		Accent: "amber",
	}
}

func (g *Game) AdmissionModel() domain.AdmissionModel {
	return domain.AdmissionPassword
}

func (g *Game) OperatorIDKind() domain.OperatorIDKind {
	return domain.IDKindSteam64
}

func (g *Game) Providers() []ports.ContentProvider {
	return g.providers
}

// RuntimeSpec defines the execution shape of a Valheim server container.
func (g *Game) RuntimeSpec(inst domain.Instance) domain.RuntimeSpec {
	env := map[string]string{
		"SERVER_NAME": inst.Name,
		"WORLD_NAME":  inst.Slug,
	}
	if inst.MOTD != "" {
		env["SERVER_PUBLIC"] = "true"
	}
	return domain.RuntimeSpec{
		Image: g.image,
		Ports: []domain.PortSpec{
			{Name: "game", Port: 2456, Protocol: "UDP"},
			{Name: "query", Port: 2457, Protocol: "UDP"},
		},
		Volumes: []domain.VolumeSpec{
			{Name: "config", MountPath: "/config", ReadOnly: false},
			{Name: "data", MountPath: "/opt/valheim", ReadOnly: false},
		},
		Env:         env,
		HealthProbe: "status.json",
	}
}

// WithBepInEx guarantees the exported profile carries denikson/BepInExPack_Valheim
// as mod #1. The server's valheim-mods list omits it (the lloesche image installs
// BepInEx itself), but an r2modman profile is unlaunchable without it.
func WithBepInEx(entries []string, version string) []string {
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e), "bepinexpack") {
			return entries
		}
	}
	if version == "" {
		version = FallbackBepInExVersion
	}
	return append([]string{"denikson/BepInExPack_Valheim/" + version}, entries...)
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

	if g.playerCountFn != nil {
		if n, err := g.playerCountFn(); err == nil {
			tele.Players = n
			tele.PlayersKnown = true
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
	var entries []string
	if g.bundleSource != nil {
		entries, _, _ = g.bundleSource(ctx, inst)
	}
	var items []domain.ContentItem
	for _, e := range entries {
		parts := strings.Split(e, "/")
		name := e
		ver := ""
		if len(parts) >= 2 {
			name = parts[1]
		}
		if len(parts) >= 3 {
			ver = parts[2]
		}
		items = append(items, domain.ContentItem{
			ID:      e,
			Name:    name,
			Version: ver,
		})
	}
	return domain.ContentSet{Items: items}, nil
}

// ExportClientBundle generates an .r2z bundle compatible with r2modman / Thunderstore.
func (g *Game) ExportClientBundle(ctx context.Context, inst domain.Instance) (domain.Bundle, error) {
	name := inst.Name
	if name == "" {
		name = "Valheim"
	}
	slug := inst.Slug
	if slug == "" {
		slug = domain.Slugify(name)
	}

	var entries []string
	var configs map[string]string
	if g.bundleSource != nil {
		var err error
		entries, configs, err = g.bundleSource(ctx, inst)
		if err != nil {
			return domain.Bundle{}, fmt.Errorf("read valheim bundle source: %w", err)
		}
	}

	modEntries := WithBepInEx(entries, FallbackBepInExVersion)
	data, err := modpack.Build(name, modEntries, configs)
	if err != nil {
		return domain.Bundle{}, fmt.Errorf("build valheim client bundle: %w", err)
	}
	return domain.Bundle{
		Filename:    fmt.Sprintf("%s-mods.r2z", slug),
		ContentType: "application/zip",
		Data:        data,
	}, nil
}

// BuildClientBundle creates a fully-populated .r2z profile from entries and config files.
func (g *Game) BuildClientBundle(profileName string, entries []string, configs map[string]string, bepInExVer string) (domain.Bundle, error) {
	modEntries := WithBepInEx(entries, bepInExVer)
	data, err := modpack.Build(profileName, modEntries, configs)
	if err != nil {
		return domain.Bundle{}, fmt.Errorf("build valheim client bundle: %w", err)
	}
	return domain.Bundle{
		Filename:    fmt.Sprintf("%s-mods.r2z", domain.Slugify(profileName)),
		ContentType: "application/zip",
		Data:        data,
	}, nil
}
