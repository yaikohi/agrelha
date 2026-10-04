package domain

import (
	"fmt"
	"time"
)

type GameID string

const (
	GameValheim   GameID = "valheim"
	GameMinecraft GameID = "minecraft"
	GameGMod      GameID = "gmod"
)

// InstanceID is the composite identity of an Instance: its game and number together.
type InstanceID struct {
	Game   GameID
	Number int
}

func NewInstanceID(game GameID, number int) InstanceID {
	return InstanceID{Game: game, Number: number}
}

func (id InstanceID) String() string {
	return fmt.Sprintf("%s-%02d", id.Game, id.Number)
}

func (id InstanceID) IsZero() bool {
	return id.Game == "" && id.Number == 0
}

// Capabilities declares what operational features a game supports.
type Capabilities struct {
	Mods               bool
	Modpacks           bool
	Configs            bool
	Backups            bool
	AdmissionPassword  bool
	AdmissionAllowlist bool
	Operators          bool
	PlayerCount        bool
}

// GameProfile defines the fixed shape of a game: identity, resource naming prefix, table, and capabilities.
type GameProfile struct {
	ID                GameID
	Prefix            string
	Table             string
	Display           Display
	Capabilities      Capabilities
	DefaultMaxPlayers int
}

var (
	MinecraftProfile = GameProfile{
		ID:     GameMinecraft,
		Prefix: "mc",
		Table:  "mc_instances",
		Display: Display{
			Name:   "Minecraft",
			Icon:   "pickaxe",
			Accent: "emerald",
		},
		Capabilities: Capabilities{
			Mods:               true,
			Modpacks:           true,
			Configs:            true,
			Backups:            true,
			AdmissionAllowlist: true,
			Operators:          true,
			PlayerCount:        true,
		},
		DefaultMaxPlayers: 20,
	}

	ValheimProfile = GameProfile{
		ID:     GameValheim,
		Prefix: "valheim",
		Table:  "valheim_instances",
		Display: Display{
			Name:   "Valheim",
			Icon:   "axe",
			Accent: "amber",
		},
		Capabilities: Capabilities{
			Mods:              true,
			Configs:           true,
			Backups:           true,
			AdmissionPassword: true,
			Operators:         true,
			PlayerCount:       true,
		},
		DefaultMaxPlayers: 10,
	}

	// Garry's Mod runs whatever a single Steam Workshop collection contains, so
	// it has no per-mod browser and no config editor. Operators and player
	// counts are deliberately absent: admin belongs to whatever addon the
	// collection ships, and A2S player queries are not implemented.
	GModProfile = GameProfile{
		ID:     GameGMod,
		Prefix: "gmod",
		Table:  "gmod_instances",
		Display: Display{
			Name:   "Garry's Mod",
			Icon:   "wrench",
			Accent: "sky",
		},
		Capabilities: Capabilities{
			Modpacks:          true,
			Backups:           true,
			AdmissionPassword: true,
		},
		DefaultMaxPlayers: 16,
	}
)

var defaultProfiles = map[GameID]GameProfile{
	GameMinecraft: MinecraftProfile,
	GameValheim:   ValheimProfile,
	GameGMod:      GModProfile,
}

// ProfileFor returns the GameProfile for a known GameID, or false if unregistered.
func ProfileFor(id GameID) (GameProfile, bool) {
	p, ok := defaultProfiles[id]
	return p, ok
}

// Profiles returns all registered default profiles.
func Profiles() []GameProfile {
	return []GameProfile{
		ValheimProfile,
		MinecraftProfile,
		GModProfile,
	}
}

type AdmissionModel string

const (
	AdmissionPassword  AdmissionModel = "password"
	AdmissionAllowlist AdmissionModel = "allowlist"
)

type OperatorIDKind string

const (
	IDKindSteam64  OperatorIDKind = "steam64"
	IDKindUsername OperatorIDKind = "username"
)

type Display struct {
	Name   string
	Icon   string
	Accent string
}

// Bundle represents an exported client pack (e.g. .r2z or .mrpack) for players.
type Bundle struct {
	Filename    string
	ContentType string
	Data        []byte
}

// ContentItem represents a single mod, pack, or plugin component.
type ContentItem struct {
	ID           string
	Name         string
	Version      string
	Description  string
	IconURL      string
	DownloadURL  string
	Dependencies []string
}

// ContentSet represents resolved content for an instance.
type ContentSet struct {
	Items []ContentItem
}

// PortSpec defines a network port exposed by a game server.
type PortSpec struct {
	Name     string
	Port     int
	Protocol string // "TCP" or "UDP"
}

// VolumeSpec defines persistent storage mounts required by a game server.
type VolumeSpec struct {
	Name      string
	MountPath string
	ReadOnly  bool
}

// RuntimeSpec describes the container and workload execution shape for an instance.
type RuntimeSpec struct {
	Image       string
	Command     []string
	Args        []string
	Ports       []PortSpec
	Env         map[string]string
	Volumes     []VolumeSpec
	HealthProbe string
}

// GameTelemetry represents real-time runtime and gameplay metrics for a game engine or instance.
type GameTelemetry struct {
	State        string            // e.g. "Up", "Pending", "Stopped", "unknown"
	Online       bool              // true if server is available / ready
	StartedAt    time.Time         // timestamp when the workload started
	Uptime       string            // human-readable uptime duration, e.g. "2h 15m"
	CPU          string            // formatted CPU consumption, e.g. "15m"
	Memory       string            // formatted Memory consumption, e.g. "1200 Mi"
	Players      int               // number of players currently connected
	PlayersKnown bool              // true if player count is tracked
	Loader       string            // active loader (e.g. "NeoForge", "Fabric")
	PackName     string            // active modpack name
	Extra        map[string]string // engine-specific telemetry metadata
}

// FormatDuration formats a duration into human-readable shorthand (e.g. "2d 5h", "3h 12m", "45m").
func FormatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	m := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, h)
	}
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
