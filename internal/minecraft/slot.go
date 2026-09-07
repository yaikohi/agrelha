package minecraft

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"agrelha/internal/gitops"
)

const (
	TypeCurseForge = "AUTO_CURSEFORGE"
	TypeNeoForge   = "NEOFORGE"
	TypeFabric     = "FABRIC"

	DefaultSlotPath = "manifests/minecraft-modded-slot.yaml"
	DefaultModsPath = "manifests/minecraft-modded-mods.yaml"
)

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

type SlotSpec struct {
	Slot          string
	Type          string
	MCVersion     string
	LoaderVersion string
	CFPageURL     string
	MOTD          string
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

func NormalizeType(t string) string {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case TypeCurseForge, "CURSEFORGE", "CF":
		return TypeCurseForge
	case TypeFabric:
		return TypeFabric
	default:
		return TypeNeoForge
	}
}

func (sp SlotSpec) Loader() string {
	switch NormalizeType(sp.Type) {
	case TypeFabric:
		return "fabric"
	case TypeCurseForge:
		return "curseforge"
	default:
		return "neoforge"
	}
}

func (sp SlotSpec) Data() map[string]string {
	loaderVer := strings.TrimSpace(sp.LoaderVersion)
	if loaderVer == "" || loaderVer == "recommended" {
		loaderVer = "latest"
	}

	d := map[string]string{
		"WORLD_SLOT": SlotName(sp.Slot),
		"TYPE":       NormalizeType(sp.Type),
	}
	if motd := strings.TrimSpace(sp.MOTD); motd != "" {
		d["MOTD"] = motd
	}

	switch NormalizeType(sp.Type) {
	case TypeCurseForge:
		d["CF_PAGE_URL"] = strings.TrimSpace(sp.CFPageURL)
	case TypeFabric:
		d["VERSION"] = strings.TrimSpace(sp.MCVersion)
		d["FABRIC_LOADER_VERSION"] = loaderVer
		d["MODRINTH_PROJECTS"] = "@/config-mods/mods.txt"
		d["MODRINTH_DOWNLOAD_DEPENDENCIES"] = "required"
		d["REMOVE_OLD_MODS"] = "TRUE"
	default:
		d["VERSION"] = strings.TrimSpace(sp.MCVersion)
		d["NEOFORGE_VERSION"] = loaderVer
		d["MODRINTH_PROJECTS"] = "@/config-mods/mods.txt"
		d["MODRINTH_DOWNLOAD_DEPENDENCIES"] = "required"
		d["REMOVE_OLD_MODS"] = "TRUE"
	}
	return d
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

func (m *SlotManager) Apply(ctx context.Context, sp SlotSpec, msg string) (bool, error) {
	return m.committer.ReplaceData(ctx, m.slotPath, sp.Data(), msg)
}

func (m *SlotManager) SwitchToCurseForge(ctx context.Context, packName, pageURL string) (bool, error) {
	slot := SlotName(packName)
	sp := SlotSpec{
		Slot:      slot,
		Type:      TypeCurseForge,
		CFPageURL: pageURL,
		MOTD:      fmt.Sprintf("%s (nf.ykhi.xyz)", packName),
	}
	msg := fmt.Sprintf("mc-slot: switch to %s (curseforge, slot=%s)", packName, slot)
	return m.Apply(ctx, sp, msg)
}

func (m *SlotManager) SwitchToModList(ctx context.Context, packName, loader, mcVersion string, slugs []string) (bool, error) {
	typ := TypeNeoForge
	if strings.EqualFold(loader, "fabric") {
		typ = TypeFabric
	}
	slot := SlotName(packName)
	sp := SlotSpec{
		Slot:      slot,
		Type:      typ,
		MCVersion: mcVersion,
		MOTD:      fmt.Sprintf("%s (nf.ykhi.xyz)", packName),
	}

	msg := fmt.Sprintf("mc-slot: switch to %s (%s, slot=%s, %d mods)", packName, strings.ToLower(typ), slot, len(slugs))
	changedSlot, err := m.Apply(ctx, sp, msg)
	if err != nil {
		return false, err
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Modpack: %s\n", packName))
	for _, s := range slugs {
		if s = strings.TrimSpace(s); s != "" {
			b.WriteString(s + "\n")
		}
	}
	changedMods, err := m.committer.ReplaceData(ctx, m.modsPath, map[string]string{"mods.txt": b.String()}, msg)
	if err != nil {
		return changedSlot, err
	}
	return changedSlot || changedMods, nil
}

func (m *SlotManager) SetVersion(ctx context.Context, cur SlotSpec, mcVersion, loaderVersion string) (bool, error) {
	if NormalizeType(cur.Type) == TypeCurseForge {
		return false, fmt.Errorf("slot %q is a CurseForge pack: its Minecraft version is pinned by the pack, not settable here", cur.Slot)
	}
	cur.MCVersion = mcVersion
	cur.LoaderVersion = loaderVersion
	msg := fmt.Sprintf("mc-slot: set MC=%s loader=%s (slot=%s)", mcVersion, loaderVersion, SlotName(cur.Slot))
	return m.Apply(ctx, cur, msg)
}
