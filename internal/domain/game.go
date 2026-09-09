// Package domain contains pure domain types, rules, and invariants.
// It imports nothing outward (no ports, adapters, or infrastructure).
package domain

type GameID string

const (
	GameValheim   GameID = "valheim"
	GameMinecraft GameID = "minecraft"
)

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
