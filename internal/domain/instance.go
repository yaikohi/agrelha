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

type MinecraftConfig struct {
	Loader      Loader
	Pack        *Pack
	MCVersion   string
	Difficulty  string
	Gamemode    string
	WorldType   string
	Seed        string
	HeapInitGiB int
}

type ValheimConfig struct {
	Password string
	Seed     string
}

type Instance struct {
	GameID     GameID
	Number     int
	Name       string
	Slug       string
	Source     Source
	Tier       ResourceTier
	Resources  Resources
	State      InstanceState
	MOTD       string
	MaxPlayers int
	LBIP       string
	CreatedBy  string
	CreatedAt  time.Time
	LastUsed   time.Time

	Minecraft *MinecraftConfig
	Valheim   *ValheimConfig
}

func (inst Instance) ID() InstanceID {
	return InstanceID{Game: inst.GameID, Number: inst.Number}
}

func (inst Instance) Password() string {
	if inst.Valheim != nil {
		return inst.Valheim.Password
	}
	return ""
}

func (inst Instance) Seed() string {
	if inst.Valheim != nil && inst.Valheim.Seed != "" {
		return inst.Valheim.Seed
	}
	if inst.Minecraft != nil && inst.Minecraft.Seed != "" {
		return inst.Minecraft.Seed
	}
	return ""
}

func (inst Instance) EffectiveResources(profile GameProfile) Resources {
	if _, ok := ProfileFor(profile.ID); !ok {
		panic(fmt.Sprintf("unknown or unregistered game ID %q", profile.ID))
	}
	if !inst.Resources.IsZero() {
		return inst.Resources
	}
	r := LegacyResources(profile.ID, inst.Tier)
	if r.IsZero() {
		panic(fmt.Sprintf("instance %d of game %q has no resources and no legacy default", inst.Number, profile.ID))
	}
	return r
}

func (inst Instance) MemoryGiB(profile GameProfile) int {
	return inst.EffectiveResources(profile).MemRequestGiB
}

func (inst Instance) MemoryLimitGiB(profile GameProfile) int {
	return inst.EffectiveResources(profile).MemLimitGiB
}

func (inst Instance) CPURequestMilli(profile GameProfile) int {
	return inst.EffectiveResources(profile).CPURequestMilli
}

func (inst Instance) CPULimitMilli(profile GameProfile) int {
	return inst.EffectiveResources(profile).CPULimitMilli
}

func (inst Instance) HeapInitMemoryGiB(profile GameProfile) int {
	if _, ok := ProfileFor(profile.ID); !ok {
		panic(fmt.Sprintf("unknown or unregistered game ID %q", profile.ID))
	}
	if inst.Minecraft != nil && inst.Minecraft.HeapInitGiB > 0 {
		return inst.Minecraft.HeapInitGiB
	}
	return LegacyHeapInitGiB(profile.ID, inst.Tier)
}

func (inst Instance) DriftsFromTier(profile GameProfile) bool {
	if inst.Resources.IsZero() {
		return false
	}
	return inst.Resources != LegacyResources(profile.ID, inst.Tier)
}

func (inst Instance) PackDefined() bool {
	return inst.Source == SourceModpack && inst.Minecraft != nil && inst.Minecraft.Pack != nil
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
	if inst.Minecraft != nil && inst.Minecraft.Pack != nil {
		provider = string(inst.Minecraft.Pack.Provider)
		name = inst.Minecraft.Pack.Name
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

func (inst *Instance) EnsureDefaults(profile GameProfile, lbBase string) {
	if _, ok := ProfileFor(profile.ID); !ok {
		panic(fmt.Sprintf("unknown or unregistered game ID %q", profile.ID))
	}
	if inst.GameID != "" && inst.GameID != profile.ID {
		panic(fmt.Sprintf("mismatched game profile %q for instance with game %q", profile.ID, inst.GameID))
	}
	inst.GameID = profile.ID
	switch profile.ID {
	case GameMinecraft:
		if inst.Minecraft == nil {
			inst.Minecraft = &MinecraftConfig{}
		}
		if inst.Minecraft.Difficulty == "" {
			inst.Minecraft.Difficulty = "normal"
		}
		if inst.Minecraft.Gamemode == "" {
			inst.Minecraft.Gamemode = "survival"
		}
		if inst.Minecraft.WorldType == "" {
			inst.Minecraft.WorldType = "default"
		}
	case GameValheim:
		if inst.Valheim == nil {
			inst.Valheim = &ValheimConfig{}
		}
	default:
		panic(fmt.Sprintf("unknown or unregistered game ID %q", profile.ID))
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
		inst.MaxPlayers = profile.DefaultMaxPlayers
		if inst.MaxPlayers <= 0 {
			inst.MaxPlayers = 20
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

func (inst Instance) DeploymentName(profile GameProfile) string {
	if _, ok := ProfileFor(profile.ID); !ok {
		panic(fmt.Sprintf("unknown or unregistered game ID %q", profile.ID))
	}
	return fmt.Sprintf("%s-%s-%02d", profile.Prefix, inst.Slug, inst.Number)
}

func (inst Instance) ServiceName(profile GameProfile) string {
	if _, ok := ProfileFor(profile.ID); !ok {
		panic(fmt.Sprintf("unknown or unregistered game ID %q", profile.ID))
	}
	return fmt.Sprintf("%s-%s-%02d", profile.Prefix, inst.Slug, inst.Number)
}

func (inst Instance) PVCName(profile GameProfile) string {
	if _, ok := ProfileFor(profile.ID); !ok {
		panic(fmt.Sprintf("unknown or unregistered game ID %q", profile.ID))
	}
	return fmt.Sprintf("%s-instance-%02d-data", profile.Prefix, inst.Number)
}

func (inst Instance) ConfigCMName(profile GameProfile) string {
	if _, ok := ProfileFor(profile.ID); !ok {
		panic(fmt.Sprintf("unknown or unregistered game ID %q", profile.ID))
	}
	return fmt.Sprintf("%s-%s-%02d-slot", profile.Prefix, inst.Slug, inst.Number)
}

func (inst Instance) ModsCMName(profile GameProfile) string {
	if _, ok := ProfileFor(profile.ID); !ok {
		panic(fmt.Sprintf("unknown or unregistered game ID %q", profile.ID))
	}
	return fmt.Sprintf("%s-%s-%02d-mods", profile.Prefix, inst.Slug, inst.Number)
}

// BackupsSubdir is the Instance's own directory on the shared backups export.
// Every Instance mounts the same PVC and the image names every archive
// worlds-<timestamp>.zip, so a shared directory would have them overwrite each
// other. It is also where the sidecars leave what they publish for agrelha to
// read back - the server build id, and the generated BepInEx configs.
func (inst Instance) BackupsSubdir() string {
	return fmt.Sprintf("%s-%02d", inst.Slug, inst.Number)
}

func (inst Instance) ConfigsCMName(profile GameProfile) string {
	if _, ok := ProfileFor(profile.ID); !ok {
		panic(fmt.Sprintf("unknown or unregistered game ID %q", profile.ID))
	}
	return fmt.Sprintf("%s-%s-%02d-configs", profile.Prefix, inst.Slug, inst.Number)
}

// ModList is an Instance's mods as the server will receive them: one body per
// Provider, because each Provider reads its own file in its own syntax. Merging
// them would need a translation that could only ever be wrong.
//
// Primary is the game's own catalogue - Modrinth for Minecraft, Thunderstore for
// Valheim. CurseForge is Minecraft-only and empty everywhere else.
type ModList struct {
	Primary    string
	CurseForge string
}

// Empty reports whether the Instance runs no mods at all.
func (m ModList) Empty() bool {
	return strings.TrimSpace(m.Primary) == "" && strings.TrimSpace(m.CurseForge) == ""
}

// HasCurseForge reports whether any entry comes from CurseForge, which is what
// decides whether the server is told to read a CurseForge list at all.
func (m ModList) HasCurseForge() bool {
	return strings.TrimSpace(m.CurseForge) != ""
}

// Env is the environment for an Instance whose mods are not yet known - the
// common case for callers that only need identity and sizing.
func (inst Instance) Env(profile GameProfile) map[string]string {
	return inst.EnvWith(profile, ModList{})
}

// EnvWith is Env for a caller that knows what the Instance runs. Only the
// CurseForge list changes the answer: a Mod list with no CurseForge entries must
// not point the server at a CurseForge file, or it would read one that is not
// there.
func (inst Instance) EnvWith(profile GameProfile, mods ModList) map[string]string {
	switch profile.ID {
	case GameValheim:
		pass := ""
		seed := ""
		if inst.Valheim != nil {
			pass = inst.Valheim.Password
			seed = inst.Valheim.Seed
		}
		env := map[string]string{
			"SERVER_NAME":   inst.Name,
			"WORLD_NAME":    inst.Slug,
			"SERVER_PASS":   pass,
			"SERVER_PUBLIC": "true",
			"BEPINEX":       boolText(!inst.IsVanilla()),
			"STATUS_HTTP":   "true",
			"SERVER_ARGS":   "-savedir /config/worlds_local",
			"TZ":            "Europe/Amsterdam",
		}
		if seed != "" {
			env["WORLD_SEED"] = seed
		}
		return env

	case GameMinecraft:
		mc := inst.Minecraft
		if mc == nil {
			mc = &MinecraftConfig{}
		}
		env := map[string]string{
			"LEVEL": inst.Slug,
		}
		if inst.MOTD != "" {
			env["MOTD"] = inst.MOTD
		}
		if mc.Difficulty != "" {
			env["DIFFICULTY"] = mc.Difficulty
		}
		if mc.Gamemode != "" {
			env["MODE"] = mc.Gamemode
		}
		if mc.WorldType != "" && mc.WorldType != "default" {
			env["LEVEL_TYPE"] = mc.WorldType
		}
		if mc.Seed != "" {
			env["SEED"] = mc.Seed
		}
		if inst.MaxPlayers > 0 {
			env["MAX_PLAYERS"] = fmt.Sprintf("%d", inst.MaxPlayers)
		}

		switch {
		case inst.PackDefined() && mc.Pack != nil && mc.Pack.Provider == ProviderCurseForge:
			env["TYPE"] = "AUTO_CURSEFORGE"
			env["CF_PAGE_URL"] = mc.Pack.Ref

		case inst.PackDefined() && mc.Pack != nil && mc.Pack.Provider == ProviderModrinth:
			env["TYPE"] = "MODRINTH"
			env["MODRINTH_MODPACK"] = mc.Pack.Ref

		case inst.Source == SourceVanilla:
			env["TYPE"] = "VANILLA"
			env["VERSION"] = mc.MCVersion

		default:
			env["VERSION"] = mc.MCVersion
			env["MODRINTH_PROJECTS"] = "@/config-mods/mods.txt"
			if mods.HasCurseForge() {
				env["CURSEFORGE_FILES"] = "@/config-mods/curseforge.txt"
			}
			env["MODRINTH_DOWNLOAD_DEPENDENCIES"] = "required"
			env["REMOVE_OLD_MODS"] = "TRUE"
			env["MODRINTH_PROJECTS_DEFAULT_VERSION_TYPE"] = "beta"
			if NormalizeLoader(string(mc.Loader)) == LoaderFabric {
				env["TYPE"] = "FABRIC"
				env["FABRIC_LOADER_VERSION"] = "latest"
			} else {
				env["TYPE"] = "NEOFORGE"
				env["NEOFORGE_VERSION"] = "latest"
			}
		}
		return env

	default:
		panic(fmt.Sprintf("unknown or unregistered game ID %q", profile.ID))
	}
}

func (inst Instance) Annotations(profile GameProfile) map[string]string {
	if _, ok := ProfileFor(profile.ID); !ok {
		panic(fmt.Sprintf("unknown or unregistered game ID %q", profile.ID))
	}
	gameID := inst.GameID
	if gameID == "" {
		gameID = profile.ID
	}
	ann := map[string]string{
		annPrefix + "instance-number": fmt.Sprintf("%d", inst.Number),
		annPrefix + "tier":            string(inst.Tier),
		annPrefix + "game":            string(gameID),
	}

	switch profile.ID {
	case GameMinecraft:
		if inst.Minecraft != nil {
			ann[annPrefix+"source"] = string(NormalizeSource(string(inst.Source)))
			ann[annPrefix+"loader"] = string(NormalizeLoader(string(inst.Minecraft.Loader)))
			if inst.Minecraft.MCVersion != "" {
				ann[annPrefix+"mc-version"] = inst.Minecraft.MCVersion
			}
			if inst.PackDefined() && inst.Minecraft.Pack != nil {
				ann[annPrefix+"pack-provider"] = string(inst.Minecraft.Pack.Provider)
				ann[annPrefix+"pack-ref"] = inst.Minecraft.Pack.Ref
				if inst.Minecraft.Pack.Name != "" {
					ann[annPrefix+"pack-name"] = inst.Minecraft.Pack.Name
				}
			}
			if inst.Minecraft.Seed != "" {
				ann[annPrefix+"seed"] = inst.Minecraft.Seed
			}
		}
	case GameValheim:
		if inst.Valheim != nil {
			if inst.Valheim.Seed != "" {
				ann[annPrefix+"seed"] = inst.Valheim.Seed
			}
		}
	default:
		panic(fmt.Sprintf("unknown or unregistered game ID %q", profile.ID))
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
func FormatGameBackupFileName(profile GameProfile, slug string, num int, tag string) string {
	if _, ok := ProfileFor(profile.ID); !ok {
		panic(fmt.Sprintf("unknown or unregistered game ID %q", profile.ID))
	}
	ts := time.Now().UTC().Format("20060102-150405")
	if tag != "" {
		return fmt.Sprintf("%s-%s-%02d-%s-%s.tar.gz", profile.Prefix, slug, num, tag, ts)
	}
	return fmt.Sprintf("%s-%s-%02d-%s.tar.gz", profile.Prefix, slug, num, ts)
}

// FormatBackupFileName generates a standard archive name for instance backups.
func FormatBackupFileName(slug string, num int, tag string) string {
	return FormatGameBackupFileName(MinecraftProfile, slug, num, tag)
}

// BackupSummary aggregates metadata for storage and backup health reporting.
type BackupSummary struct {
	Count      int
	TotalSize  int64
	LatestName string
	LatestSize int64
	LatestAt   time.Time
}

var safeBackupName = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9-]+-\d{2}-[a-zA-Z0-9_-]+\.tar\.gz$`)

// IsSafeBackupFileName checks whether an archive name matches the standard instance backup pattern.
func IsSafeBackupFileName(name string) bool {
	if !safeBackupName.MatchString(name) {
		return false
	}
	idx := strings.Index(name, "-")
	if idx <= 0 {
		return false
	}
	prefix := name[:idx]
	for _, p := range defaultProfiles {
		if p.Prefix == prefix {
			return true
		}
	}
	return false
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
	if !b.Known() {
		return false
	}
	return b.Installed != b.Latest
}

// Known reports whether the build report carries a usable answer at all.
func (b ServerBuild) Known() bool {
	return isBuildID(b.Installed) && isBuildID(b.Latest)
}

func isBuildID(v string) bool {
	v = strings.TrimSpace(v)
	return v != "" && v != "0"
}
