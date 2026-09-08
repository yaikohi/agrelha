package minecraft

import (
	"fmt"
	"strings"
	"time"

	"agrelha/internal/store"
)

type ResourceTier string

const (
	TierSmall  ResourceTier = "small"  // 4 GiB
	TierMedium ResourceTier = "medium" // 8 GiB
	TierLarge  ResourceTier = "large"  // 12 GiB
)

func (t ResourceTier) MemoryGiB() int {
	switch t {
	case TierSmall:
		return 4
	case TierMedium:
		return 8
	case TierLarge:
		return 12
	default:
		return 8
	}
}

func NormalizeTier(t string) ResourceTier {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case string(TierSmall):
		return TierSmall
	case string(TierLarge):
		return TierLarge
	default:
		return TierMedium
	}
}

type InstanceState string

const (
	StateRunning      InstanceState = "running"
	StateStopped      InstanceState = "stopped"
	StateProvisioning InstanceState = "provisioning"
	StateError        InstanceState = "error"
)

const (
	MaxInstances   = 4
	MaxRunning     = 2
	TotalBudgetGiB = 24
)

type Instance struct {
	Number     int
	Name       string
	Slug       string
	Seed       string
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

func AssignLBIP(number int) string {
	if number < 1 || number > 4 {
		return "192.168.20.225"
	}
	return fmt.Sprintf("192.168.20.%d", 224+number)
}

func (inst *Instance) EnsureDefaults() {
	if inst.Slug == "" {
		inst.Slug = SlotName(inst.Name)
	}
	if inst.Tier == "" {
		inst.Tier = TierMedium
	}
	if inst.State == "" {
		inst.State = StateStopped
	}
	if inst.MaxPlayers <= 0 {
		inst.MaxPlayers = 20
	}
	if inst.Difficulty == "" {
		inst.Difficulty = "normal"
	}
	if inst.Gamemode == "" {
		inst.Gamemode = "survival"
	}
	if inst.WorldType == "" {
		inst.WorldType = "default"
	}
	if inst.LBIP == "" && inst.Number > 0 {
		inst.LBIP = AssignLBIP(inst.Number)
	}
	if inst.MOTD == "" {
		inst.MOTD = fmt.Sprintf("%s (%s)", inst.Name, inst.LBIP)
	}
}

func (inst Instance) DeploymentName() string {
	return fmt.Sprintf("mc-%s-%02d", inst.Slug, inst.Number)
}

func (inst Instance) ServiceName() string {
	return fmt.Sprintf("mc-%s-%02d", inst.Slug, inst.Number)
}

func (inst Instance) PVCName() string {
	return fmt.Sprintf("mc-instance-%02d-data", inst.Number)
}

// ConfigCMName is the Instance's config ConfigMap. The "-slot" suffix in the
// object name is legacy (CONTEXT.md: Slot is retired) and is kept only because
// renaming it would recreate the ConfigMap and restart every Instance.
func (inst Instance) ConfigCMName() string {
	return fmt.Sprintf("mc-%s-%02d-slot", inst.Slug, inst.Number)
}

func (inst Instance) ModsCMName() string {
	return fmt.Sprintf("mc-%s-%02d-mods", inst.Slug, inst.Number)
}

func (inst Instance) ConfigsCMName() string {
	return fmt.Sprintf("mc-%s-%02d-configs", inst.Slug, inst.Number)
}

func (inst Instance) PackDefined() bool {
	return inst.Source == SourceModpack && inst.Pack != nil
}

// CanSetLoader and CanSetVersion encode the core invariant: when an Instance is
// defined by a Pack, its Loader and Minecraft version are FACTS READ FROM THE
// PACK, not settings. Offering to change them is offering to break the Instance
// — which is exactly how the Ducktopia pack was silently replaced by a bare
// Fabric server.
func (inst Instance) CanSetLoader() bool  { return !inst.PackDefined() }
func (inst Instance) CanSetVersion() bool { return !inst.PackDefined() }

// PackOwnedFieldErr explains why a pack-defined field cannot be changed.
func (inst Instance) PackOwnedFieldErr(field string) error {
	return fmt.Errorf("%s is defined by the %s pack %q: the pack decides it, so it cannot be changed here — create a new instance to run different content",
		field, inst.Pack.Provider, inst.Pack.Name)
}

func (inst Instance) MemoryGiB() int {
	return inst.Tier.MemoryGiB()
}

func (inst Instance) MemoryLimitGiB() int {
	switch inst.Tier {
	case TierSmall:
		return 6
	case TierLarge:
		return 16
	default:
		return 10
	}
}

func (inst Instance) HeapInitMemoryGiB() int {
	switch inst.Tier {
	case TierSmall:
		return 3
	case TierLarge:
		return 10
	default:
		return 6
	}
}

func (inst Instance) Env() map[string]string {
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
	ann := map[string]string{
		annPrefix + "instance-number": fmt.Sprintf("%d", inst.Number),
		annPrefix + "source":          string(NormalizeSource(string(inst.Source))),
		annPrefix + "loader":          string(NormalizeLoader(string(inst.Loader))),
		annPrefix + "tier":            string(inst.Tier),
	}
	if inst.MCVersion != "" {
		ann[annPrefix+"mc-version"] = inst.MCVersion
	}
	if inst.Seed != "" {
		ann[annPrefix+"seed"] = inst.Seed
	}
	if inst.PackDefined() {
		ann[annPrefix+"pack-provider"] = string(inst.Pack.Provider)
		ann[annPrefix+"pack-ref"] = inst.Pack.Ref
		if inst.Pack.Name != "" {
			ann[annPrefix+"pack-name"] = inst.Pack.Name
		}
	}
	return ann
}

func InstanceFromRecord(r store.InstanceRecord) Instance {
	inst := Instance{
		Number:     r.Number,
		Name:       r.Name,
		Slug:       r.Slug,
		Seed:       r.Seed,
		Loader:     NormalizeLoader(r.Loader),
		Source:     NormalizeSource(r.Source),
		MCVersion:  r.MCVersion,
		Tier:       NormalizeTier(r.Tier),
		State:      InstanceState(r.State),
		MOTD:       r.MOTD,
		Difficulty: r.Difficulty,
		Gamemode:   r.Gamemode,
		WorldType:  r.WorldType,
		MaxPlayers: r.MaxPlayers,
		LBIP:       r.LBIP,
		CreatedAt:  r.CreatedAt,
		LastUsed:   r.LastUsed,
	}
	if inst.Source == SourceModpack && r.PackRef != "" {
		provider := Provider(r.PackProvider)
		if provider == "" {
			provider = ProviderCurseForge
		}
		inst.Pack = &Pack{
			Provider: provider,
			Ref:      r.PackRef,
			Name:     r.Pack,
		}
	}
	inst.EnsureDefaults()
	return inst
}

func (inst Instance) ToRecord() store.InstanceRecord {
	rec := store.InstanceRecord{
		Number:     inst.Number,
		Name:       inst.Name,
		Slug:       inst.Slug,
		Seed:       inst.Seed,
		Loader:     string(inst.Loader),
		Source:     string(inst.Source),
		MCVersion:  inst.MCVersion,
		Tier:       string(inst.Tier),
		State:      string(inst.State),
		MOTD:       inst.MOTD,
		Difficulty: inst.Difficulty,
		Gamemode:   inst.Gamemode,
		WorldType:  inst.WorldType,
		MaxPlayers: inst.MaxPlayers,
		LBIP:       inst.LBIP,
		CreatedAt:  inst.CreatedAt,
		LastUsed:   inst.LastUsed,
	}
	if inst.Pack != nil {
		rec.Pack = inst.Pack.Name
		rec.PackProvider = string(inst.Pack.Provider)
		rec.PackRef = inst.Pack.Ref
	}
	return rec
}
