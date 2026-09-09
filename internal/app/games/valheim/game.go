package valheim

import (
	"context"
	"fmt"
	"strings"

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
	providers []ports.ContentProvider
	image     string
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

func (g *Game) ResolveContent(ctx context.Context, inst domain.Instance) (domain.ContentSet, error) {
	return domain.ContentSet{}, nil
}

// ExportClientBundle generates an .r2z bundle compatible with r2modman / Thunderstore.
func (g *Game) ExportClientBundle(ctx context.Context, inst domain.Instance) (domain.Bundle, error) {
	// Build an empty or default bundle; full bundle with configs is built via BuildClientBundle
	data, err := modpack.Build(inst.Name, WithBepInEx(nil, FallbackBepInExVersion), nil)
	if err != nil {
		return domain.Bundle{}, fmt.Errorf("build valheim client bundle: %w", err)
	}
	return domain.Bundle{
		Filename:    fmt.Sprintf("%s-mods.r2z", inst.Slug),
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
