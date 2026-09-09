package instances

import (
	"agrelha/internal/domain"
	"testing"
)

// A Pack owns its Instance's Loader and Minecraft version (CONTEXT.md). This is
// the invariant whose absence let a NeoForge pack be created on a Fabric
// instance and, earlier, silently replaced a pack with a bare loader server.
func TestPackDefinedInstanceRefusesLoaderAndVersionChanges(t *testing.T) {
	packed := domain.Instance{
		Number: 1, Name: "ayyy", Slug: "ayyy",
		Source: domain.SourceModpack, Loader: domain.LoaderNeoForge, MCVersion: "1.21.1",
		Pack: &domain.Pack{Provider: domain.ProviderCurseForge, Ref: "https://cf/fluxweave", Name: "Fluxweave"},
	}
	if packed.CanSetLoader() || packed.CanSetVersion() {
		t.Fatal("a pack-defined domain.Instance must report loader/version as not settable")
	}
	if err := packed.PackOwnedFieldErr("Minecraft version"); err == nil {
		t.Fatal("expected an explanatory error")
	}

	free := domain.Instance{
		Number: 2, Name: "plain", Slug: "plain",
		Source: domain.SourceModlist, Loader: domain.LoaderFabric, MCVersion: "1.21.1",
	}
	if !free.CanSetLoader() || !free.CanSetVersion() {
		t.Fatal("a mod-list domain.Instance must allow loader/version changes")
	}
}

// WORLD_SLOT was vestigial: it duplicated LEVEL and selected nothing once each
// Instance got its own PVC. LEVEL is what actually places the World.
func TestInstanceEnvUsesLevelAndNotWorldSlot(t *testing.T) {
	env := domain.Instance{Number: 1, Name: "ayyy", Slug: "ayyy", Source: domain.SourceVanilla, MCVersion: "1.21.1"}.Env()

	if env["LEVEL"] != "ayyy" {
		t.Fatalf("LEVEL = %q, want ayyy — it places the World", env["LEVEL"])
	}
	if _, ok := env["WORLD_SLOT"]; ok {
		t.Fatal("WORLD_SLOT is retired; it duplicated LEVEL and selected nothing")
	}
}
