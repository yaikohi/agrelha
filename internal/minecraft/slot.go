package minecraft

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"agrelha/internal/gitops"
)

type Source string
type Provider string
type Loader string

const (
	SourceModpack Source = "modpack"
	SourceModlist Source = "modlist"
	SourceVanilla Source = "vanilla"

	ProviderCurseForge Provider = "curseforge"
	ProviderModrinth   Provider = "modrinth"

	LoaderFabric   Loader = "fabric"
	LoaderNeoForge Loader = "neoforge"

	DefaultSlotPath = "manifests/minecraft-modded/slot.yaml"
	DefaultModsPath = "manifests/minecraft-modded/mods.yaml"

	annPrefix = "agrelha.ykhi.xyz/"
)

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

type Pack struct {
	Provider Provider
	Ref      string
	Name     string
}

type Slot struct {
	Name      string
	Loader    Loader
	Source    Source
	Pack      *Pack
	MCVersion string
	MOTD      string
}

func SlotName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugUnsafe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		return "default"
	}
	return s
}

func NormalizeLoader(l string) Loader {
	if strings.EqualFold(strings.TrimSpace(l), string(LoaderFabric)) {
		return LoaderFabric
	}
	return LoaderNeoForge
}

func NormalizeSource(s string) Source {
	switch Source(strings.ToLower(strings.TrimSpace(s))) {
	case SourceModpack:
		return SourceModpack
	case SourceVanilla:
		return SourceVanilla
	default:
		return SourceModlist
	}
}

// PackDefined reports whether the pack, not the operator, decides this slot's
// loader, Minecraft version and mod set.
func (s Slot) PackDefined() bool {
	return s.Source == SourceModpack && s.Pack != nil
}

// CanSetLoader and CanSetVersion encode the core invariant: for a pack-defined
// slot the loader and Minecraft version are facts read from the pack, not
// settings. Offering to change them is what silently destroyed a slot before.
func (s Slot) CanSetLoader() bool  { return !s.PackDefined() }
func (s Slot) CanSetVersion() bool { return !s.PackDefined() }

// Env renders the itzg container variables. TYPE is derived here and nowhere
// else — it is an implementation detail, never domain vocabulary.
func (s Slot) Env() map[string]string {
	slot := SlotName(s.Name)
	env := map[string]string{
		// WORLD_SLOT is the slot's identity (agrelha reads it back); LEVEL is
		// what itzg consumes to place the world at /data/<slot>. Same value,
		// two layers: identity vs rendering.
		"WORLD_SLOT": slot,
		"LEVEL":      slot,
	}
	if motd := strings.TrimSpace(s.MOTD); motd != "" {
		env["MOTD"] = motd
	}

	switch {
	case s.PackDefined() && s.Pack.Provider == ProviderCurseForge:
		env["TYPE"] = "AUTO_CURSEFORGE"
		env["CF_PAGE_URL"] = s.Pack.Ref
		// No VERSION: it constrains pack-file selection and breaks the install.

	case s.PackDefined() && s.Pack.Provider == ProviderModrinth:
		env["TYPE"] = "MODRINTH"
		env["MODRINTH_MODPACK"] = s.Pack.Ref

	case s.Source == SourceVanilla:
		env["TYPE"] = "VANILLA"
		env["VERSION"] = s.MCVersion

	default:
		env["VERSION"] = s.MCVersion
		env["MODRINTH_PROJECTS"] = "@/config-mods/mods.txt"
		env["MODRINTH_DOWNLOAD_DEPENDENCIES"] = "required"
		env["REMOVE_OLD_MODS"] = "TRUE"
		// Modpack mod lists routinely contain mods whose newest build for a
		// given MC version is still beta. With the default (release) those are
		// a HARD failure ("No candidate versions ... matched versionType"),
		// which the '?' optional marker does NOT cover — it only handles
		// projects that cannot be found at all.
		env["MODRINTH_PROJECTS_DEFAULT_VERSION_TYPE"] = "beta"
		if NormalizeLoader(string(s.Loader)) == LoaderFabric {
			env["TYPE"] = "FABRIC"
			env["FABRIC_LOADER_VERSION"] = "latest"
		} else {
			env["TYPE"] = "NEOFORGE"
			env["NEOFORGE_VERSION"] = "latest"
		}
	}
	return env
}

// Annotations renders the domain intent. Stored in metadata.annotations so that
// envFrom (which reads only .data) never exposes it to the container.
func (s Slot) Annotations() map[string]string {
	ann := map[string]string{
		annPrefix + "source": string(NormalizeSource(string(s.Source))),
		annPrefix + "loader": string(NormalizeLoader(string(s.Loader))),
	}
	if v := strings.TrimSpace(s.MCVersion); v != "" {
		ann[annPrefix+"mc-version"] = v
	}
	if s.PackDefined() {
		ann[annPrefix+"pack-provider"] = string(s.Pack.Provider)
		ann[annPrefix+"pack-ref"] = s.Pack.Ref
		if s.Pack.Name != "" {
			ann[annPrefix+"pack-name"] = s.Pack.Name
		}
	}
	return ann
}

// SlotFromAnnotations reconstructs the domain model from a live ConfigMap.
func SlotFromAnnotations(ann map[string]string, data map[string]string) Slot {
	get := func(k string) string { return strings.TrimSpace(ann[annPrefix+k]) }

	s := Slot{
		Name:      strings.TrimSpace(data["WORLD_SLOT"]),
		Loader:    NormalizeLoader(get("loader")),
		Source:    NormalizeSource(get("source")),
		MCVersion: get("mc-version"),
		MOTD:      strings.TrimSpace(data["MOTD"]),
	}
	if s.Source == SourceModpack {
		ref := get("pack-ref")
		if ref == "" {
			ref = strings.TrimSpace(data["CF_PAGE_URL"])
		}
		if ref != "" {
			provider := Provider(get("pack-provider"))
			if provider == "" {
				provider = ProviderCurseForge
			}
			s.Pack = &Pack{Provider: provider, Ref: ref, Name: get("pack-name")}
		}
	}
	if s.MCVersion == "" {
		s.MCVersion = strings.TrimSpace(data["VERSION"])
	}
	return s
}

type SlotManager struct {
	committer *gitops.Committer
	slotPath  string
	modsPath  string
}

func NewSlotManager(c *gitops.Committer, slotPath, modsPath string) *SlotManager {
	if slotPath == "" {
		slotPath = DefaultSlotPath
	}
	if modsPath == "" {
		modsPath = DefaultModsPath
	}
	return &SlotManager{committer: c, slotPath: slotPath, modsPath: modsPath}
}

func (m *SlotManager) ModsPath() string { return m.modsPath }
func (m *SlotManager) SlotPath() string { return m.slotPath }

func (m *SlotManager) Apply(ctx context.Context, s Slot, msg string) (bool, error) {
	return m.committer.ReplaceSlot(ctx, m.slotPath, s.Env(), s.Annotations(), msg)
}

func (m *SlotManager) SwitchToPack(ctx context.Context, p Pack, mcVersion string, loader Loader) (bool, error) {
	name := p.Name
	if name == "" {
		name = p.Ref
	}
	s := Slot{
		Name:      SlotName(name),
		Loader:    loader,
		Source:    SourceModpack,
		Pack:      &p,
		MCVersion: mcVersion,
		MOTD:      fmt.Sprintf("%s (nf.ykhi.xyz)", name),
	}
	msg := fmt.Sprintf("mc-slot: switch to %s pack %q (slot=%s)", p.Provider, name, s.Name)
	return m.Apply(ctx, s, msg)
}

func (m *SlotManager) SwitchToModList(ctx context.Context, packName string, loader Loader, mcVersion string, slugs []string) (bool, error) {
	s := Slot{
		Name:      SlotName(packName),
		Loader:    loader,
		Source:    SourceModlist,
		MCVersion: mcVersion,
		MOTD:      fmt.Sprintf("%s (nf.ykhi.xyz)", packName),
	}

	msg := fmt.Sprintf("mc-slot: switch to %s (%s modlist, slot=%s, %d mods)", packName, loader, s.Name, len(slugs))
	changedSlot, err := m.Apply(ctx, s, msg)
	if err != nil {
		return false, err
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Modpack: %s\n", packName))
	for _, sl := range slugs {
		if sl = strings.TrimSpace(sl); sl != "" {
			b.WriteString(sl + "\n")
		}
	}
	changedMods, err := m.committer.ReplaceData(ctx, m.modsPath, map[string]string{"mods.txt": b.String()}, msg)
	if err != nil {
		return changedSlot, err
	}
	return changedSlot || changedMods, nil
}

func (m *SlotManager) SetVersion(ctx context.Context, cur Slot, mcVersion string) (bool, error) {
	if !cur.CanSetVersion() {
		return false, fmt.Errorf("slot %q is defined by the %s pack %q: its Minecraft version comes from the pack and cannot be set here",
			cur.Name, cur.Pack.Provider, cur.Pack.Ref)
	}
	cur.MCVersion = mcVersion
	msg := fmt.Sprintf("mc-slot: set Minecraft %s (slot=%s)", mcVersion, SlotName(cur.Name))
	return m.Apply(ctx, cur, msg)
}

func (m *SlotManager) SetLoader(ctx context.Context, cur Slot, loader Loader) (bool, error) {
	if !cur.CanSetLoader() {
		return false, fmt.Errorf("slot %q is defined by the %s pack %q: the pack decides the mod loader, so it cannot be switched here",
			cur.Name, cur.Pack.Provider, cur.Pack.Ref)
	}
	cur.Loader = loader
	msg := fmt.Sprintf("mc-slot: set loader %s (slot=%s)", loader, SlotName(cur.Name))
	return m.Apply(ctx, cur, msg)
}
