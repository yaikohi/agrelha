package minecraft

import (
	"agrelha/internal/domain"
)

// Content vocabulary shared by every Minecraft server model. See CONTEXT.md:
// Loader is the engine, Source is how content is defined, and Provider is who
// distributes a Pack. They are deliberately orthogonal — conflating them into
// itzg's single TYPE is what once let a CurseForge pack be treated as a "type"
// of server and silently destroyed.
type Source = domain.Source
type Provider = domain.Provider
type Loader = domain.Loader

const (
	SourceModpack = domain.SourceModpack
	SourceModlist = domain.SourceModlist
	SourceVanilla = domain.SourceVanilla

	ProviderCurseForge   = domain.ProviderCurseForge
	ProviderModrinth     = domain.ProviderModrinth
	ProviderThunderstore = domain.ProviderThunderstore

	LoaderFabric   = domain.LoaderFabric
	LoaderNeoForge = domain.LoaderNeoForge
	LoaderVanilla  = domain.LoaderVanilla

	// Annotation namespace. These are write-only documentation for humans
	// reading the manifests in git — Instance state is rebuilt from the store,
	// never from annotations — so this can change without a migration.
	annPrefix = "agrelha.dev/"

	// DefaultModsPath is the fallback location of the shared mods.txt in the
	// ops repo. Per-Instance mod lists live under instance-NN/mods.yaml.
	DefaultModsPath = "manifests/minecraft-modded/mods.yaml"
)

// Pack is a published, versioned mod collection. When a server has one, the Pack
// decides its Loader and Minecraft version.
type Pack = domain.Pack

// SlotName sanitises a display name into a stable identifier.
func SlotName(s string) string {
	return domain.Slugify(s)
}

func NormalizeLoader(l string) Loader {
	return domain.NormalizeLoader(l)
}

func NormalizeSource(s string) Source {
	return domain.NormalizeSource(s)
}
