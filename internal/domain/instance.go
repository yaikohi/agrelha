package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type InstanceState string

const (
	StateRunning      InstanceState = "running"
	StateStopped      InstanceState = "stopped"
	StateProvisioning InstanceState = "provisioning"
	StateError        InstanceState = "error"
)

const annPrefix = "agrelha.dev/"

type Instance struct {
	GameID     GameID
	Number     int
	Name       string
	Slug       string
	Seed       string
	Password   string
	Loader     Loader
	Source     Source
	Pack       *Pack
	MCVersion  string
	Tier       ResourceTier
	State      InstanceState
	MOTD       string
	Difficulty string
	Gamemode   string
	WorldType  string
	MaxPlayers int
	LBIP       string
	CreatedAt  time.Time
	LastUsed   time.Time
}

func (inst Instance) MemoryGiB() int {
	if inst.GameID == GameValheim {
		switch inst.Tier {
		case TierSmall:
			return 4
		case TierLarge:
			return 8
		default:
			return 6
		}
	}
	return inst.Tier.MemoryGiB()
}

func (inst Instance) MemoryLimitGiB() int {
	if inst.GameID == GameValheim {
		switch inst.Tier {
		case TierSmall:
			return 5
		case TierLarge:
			return 10
		default:
			return 7
		}
	}
	return inst.Tier.MemoryLimitGiB()
}

func (inst Instance) HeapInitMemoryGiB() int {
	if inst.GameID == GameValheim {
		return 0
	}
	return inst.Tier.HeapInitMemoryGiB()
}

func (inst Instance) PackDefined() bool {
	return inst.Source == SourceModpack && inst.Pack != nil
}

// CanSetLoader and CanSetVersion encode the core invariant: when an Instance is
// defined by a Pack, its Loader and Minecraft version are FACTS READ FROM THE
// PACK, not settings.
// IsVanilla reports whether the Instance runs without a Loader. On Valheim that
// means no BepInEx, which is the only way Steam achievements stay earnable.
func (inst Instance) IsVanilla() bool { return inst.Source == SourceVanilla }

// CanInstallMods is false for a Vanilla Instance: gaining mods would revoke the
// achievements its players earned against the promise Vanilla made, so it is a
// different server (CONTEXT.md) reached by duplicating, not editing.
func (inst Instance) CanInstallMods() bool { return !inst.IsVanilla() }

// VanillaImmutableErr explains why a Vanilla Instance refuses mods.
func (inst Instance) VanillaImmutableErr() error {
	return fmt.Errorf("%s is a vanilla world: adding mods would disable achievements for everyone who plays it. Duplicate it to get a modded copy", inst.Name)
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func (inst Instance) CanSetLoader() bool  { return !inst.PackDefined() }
func (inst Instance) CanSetVersion() bool { return !inst.PackDefined() }

// PackOwnedFieldErr explains why a pack-defined field cannot be changed.
func (inst Instance) PackOwnedFieldErr(field string) error {
	provider := ""
	name := ""
	if inst.Pack != nil {
		provider = string(inst.Pack.Provider)
		name = inst.Pack.Name
	}
	return fmt.Errorf("%s is defined by the %s pack %q: the pack decides it, so it cannot be changed here — create a new instance to run different content",
		field, provider, name)
}

// AssignLBIP gives Instance N the Nth address after base.
func AssignLBIP(base string, number int) string {
	if base == "" {
		return ""
	}
	i := strings.LastIndex(base, ".")
	if i < 0 || number < 1 {
		return base
	}
	last, err := strconv.Atoi(base[i+1:])
	if err != nil {
		return base
	}
	return fmt.Sprintf("%s.%d", base[:i], last+number)
}

func (inst *Instance) EnsureDefaults(lbBase string) {
	if inst.GameID == "" {
		inst.GameID = GameMinecraft
	}
	if inst.Slug == "" {
		inst.Slug = Slugify(inst.Name)
	}
	if inst.Tier == "" {
		inst.Tier = TierMedium
	}
	if inst.State == "" {
		inst.State = StateStopped
	}
	if inst.MaxPlayers <= 0 {
		if inst.GameID == GameValheim {
			inst.MaxPlayers = 10
		} else {
			inst.MaxPlayers = 20
		}
	}
	if inst.GameID == GameMinecraft {
		if inst.Difficulty == "" {
			inst.Difficulty = "normal"
		}
		if inst.Gamemode == "" {
			inst.Gamemode = "survival"
		}
		if inst.WorldType == "" {
			inst.WorldType = "default"
		}
	}
	if inst.LBIP == "" && inst.Number > 0 {
		inst.LBIP = AssignLBIP(lbBase, inst.Number)
	}
	if inst.MOTD == "" {
		if inst.LBIP != "" {
			inst.MOTD = fmt.Sprintf("%s (%s)", inst.Name, inst.LBIP)
		} else {
			inst.MOTD = inst.Name
		}
	}
}

func (inst Instance) DeploymentName() string {
	if inst.GameID == GameValheim {
		return fmt.Sprintf("valheim-%s-%02d", inst.Slug, inst.Number)
	}
	return fmt.Sprintf("mc-%s-%02d", inst.Slug, inst.Number)
}

func (inst Instance) ServiceName() string {
	if inst.GameID == GameValheim {
		return fmt.Sprintf("valheim-%s-%02d", inst.Slug, inst.Number)
	}
	return fmt.Sprintf("mc-%s-%02d", inst.Slug, inst.Number)
}

func (inst Instance) PVCName() string {
	if inst.GameID == GameValheim {
		return fmt.Sprintf("valheim-instance-%02d-data", inst.Number)
	}
	return fmt.Sprintf("mc-instance-%02d-data", inst.Number)
}

func (inst Instance) ConfigCMName() string {
	if inst.GameID == GameValheim {
		return fmt.Sprintf("valheim-%s-%02d-slot", inst.Slug, inst.Number)
	}
	return fmt.Sprintf("mc-%s-%02d-slot", inst.Slug, inst.Number)
}

func (inst Instance) ModsCMName() string {
	if inst.GameID == GameValheim {
		return fmt.Sprintf("valheim-%s-%02d-mods", inst.Slug, inst.Number)
	}
	return fmt.Sprintf("mc-%s-%02d-mods", inst.Slug, inst.Number)
}

func (inst Instance) ConfigsCMName() string {
	if inst.GameID == GameValheim {
		return fmt.Sprintf("valheim-%s-%02d-configs", inst.Slug, inst.Number)
	}
	return fmt.Sprintf("mc-%s-%02d-configs", inst.Slug, inst.Number)
}

func (inst Instance) Env() map[string]string {
	if inst.GameID == GameValheim {
		env := map[string]string{
			"SERVER_NAME":   inst.Name,
			"WORLD_NAME":    inst.Slug,
			"SERVER_PASS":   inst.Password,
			"SERVER_PUBLIC": "true",
			"BEPINEX":       boolText(!inst.IsVanilla()),
			"STATUS_HTTP":   "true",
			"SERVER_ARGS":   "-savedir /config/worlds_local",
			"TZ":            "Europe/Amsterdam",
		}
		if inst.Seed != "" {
			env["WORLD_SEED"] = inst.Seed
		}
		return env
	}

	env := map[string]string{
		"LEVEL": inst.Slug,
	}
	if inst.MOTD != "" {
		env["MOTD"] = inst.MOTD
	}
	if inst.Difficulty != "" {
		env["DIFFICULTY"] = inst.Difficulty
	}
	if inst.Gamemode != "" {
		env["MODE"] = inst.Gamemode
	}
	if inst.WorldType != "" && inst.WorldType != "default" {
		env["LEVEL_TYPE"] = inst.WorldType
	}
	if inst.Seed != "" {
		env["SEED"] = inst.Seed
	}
	if inst.MaxPlayers > 0 {
		env["MAX_PLAYERS"] = fmt.Sprintf("%d", inst.MaxPlayers)
	}

	switch {
	case inst.PackDefined() && inst.Pack.Provider == ProviderCurseForge:
		env["TYPE"] = "AUTO_CURSEFORGE"
		env["CF_PAGE_URL"] = inst.Pack.Ref

	case inst.PackDefined() && inst.Pack.Provider == ProviderModrinth:
		env["TYPE"] = "MODRINTH"
		env["MODRINTH_MODPACK"] = inst.Pack.Ref

	case inst.Source == SourceVanilla:
		env["TYPE"] = "VANILLA"
		env["VERSION"] = inst.MCVersion

	default:
		env["VERSION"] = inst.MCVersion
		env["MODRINTH_PROJECTS"] = "@/config-mods/mods.txt"
		env["MODRINTH_DOWNLOAD_DEPENDENCIES"] = "required"
		env["REMOVE_OLD_MODS"] = "TRUE"
		env["MODRINTH_PROJECTS_DEFAULT_VERSION_TYPE"] = "beta"
		if NormalizeLoader(string(inst.Loader)) == LoaderFabric {
			env["TYPE"] = "FABRIC"
			env["FABRIC_LOADER_VERSION"] = "latest"
		} else {
			env["TYPE"] = "NEOFORGE"
			env["NEOFORGE_VERSION"] = "latest"
		}
	}
	return env
}

func (inst Instance) Annotations() map[string]string {
	gameID := inst.GameID
	if gameID == "" {
		gameID = GameMinecraft
	}
	ann := map[string]string{
		annPrefix + "instance-number": fmt.Sprintf("%d", inst.Number),
		annPrefix + "tier":            string(inst.Tier),
	}
	if inst.GameID != "" {
		ann[annPrefix+"game"] = string(inst.GameID)
	}
	if gameID == GameMinecraft {
		ann[annPrefix+"source"] = string(NormalizeSource(string(inst.Source)))
		ann[annPrefix+"loader"] = string(NormalizeLoader(string(inst.Loader)))
		if inst.MCVersion != "" {
			ann[annPrefix+"mc-version"] = inst.MCVersion
		}
		if inst.PackDefined() {
			ann[annPrefix+"pack-provider"] = string(inst.Pack.Provider)
			ann[annPrefix+"pack-ref"] = inst.Pack.Ref
			if inst.Pack.Name != "" {
				ann[annPrefix+"pack-name"] = inst.Pack.Name
			}
		}
	}
	if inst.Seed != "" {
		ann[annPrefix+"seed"] = inst.Seed
	}
	return ann
}

// BackupFile represents an existing archive on disk for an instance.
type BackupFile struct {
	Name      string
	SizeBytes int64
	CreatedAt string
}

// FormatGameBackupFileName generates a standard archive name for instance backups for a specific game.
func FormatGameBackupFileName(gameID GameID, slug string, num int, tag string) string {
	prefix := "mc"
	if gameID == GameValheim {
		prefix = "valheim"
	}
	ts := time.Now().UTC().Format("20060102-150405")
	if tag != "" {
		return fmt.Sprintf("%s-%s-%02d-%s-%s.tar.gz", prefix, slug, num, tag, ts)
	}
	return fmt.Sprintf("%s-%s-%02d-%s.tar.gz", prefix, slug, num, ts)
}

// FormatBackupFileName generates a standard archive name for instance backups.
func FormatBackupFileName(slug string, num int, tag string) string {
	return FormatGameBackupFileName(GameMinecraft, slug, num, tag)
}

// BackupSummary aggregates metadata for storage and backup health reporting.
type BackupSummary struct {
	Count      int
	TotalSize  int64
	LatestName string
	LatestSize int64
	LatestAt   time.Time
}

var safeBackupName = regexp.MustCompile(`^(mc|valheim)-[a-z0-9-]+-\d{2}-[a-zA-Z0-9_-]+\.tar\.gz$`)

// IsSafeBackupFileName checks whether an archive name matches the standard instance backup pattern.
func IsSafeBackupFileName(name string) bool {
	return safeBackupName.MatchString(name)
}

// ServerBuild is what the game's own store says about the binaries an Instance
// is running, as opposed to its Mod list. A Server update is available when the
// Instance is behind Latest.
//
// It is reported by the server itself rather than derived by agrelha: the
// Valheim image's updater contacts Steam only when it is already idle enough to
// install, so while players are connected an available update leaves no trace
// anywhere agrelha can read.
type ServerBuild struct {
	Installed string
	Latest    string
	CheckedAt time.Time
}

// UpdateAvailable reports whether the store carries newer binaries than the
// Instance is running. Unknown on either side means no claim: a failed upstream
// check must never be rendered as "up to date".
func (b ServerBuild) UpdateAvailable() bool {
	if b.Installed == "" || b.Latest == "" {
		return false
	}
	return b.Installed != b.Latest
}

// Known reports whether the build report carries a usable answer at all.
func (b ServerBuild) Known() bool {
	return b.Installed != "" && b.Latest != ""
}
