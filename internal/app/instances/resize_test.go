package instances

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/infra/manifests"
	"agrelha/internal/infra/store"
)

// installed stands in for the live mods ConfigMap. Production always wires a
// mods reader (WithModsReader), so GetInstalledMods never falls back to parsing
// the state store - and mockStateStore could not serve that fallback anyway,
// since PutTree stores only Raw while the real git adapter also parses Data.
type installedMods struct{ lines []string }

func newResizeManager(t *testing.T) (*InstanceManager, *mockStateStore, *installedMods, context.Context) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "resize.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	state := newMockStateStore()
	mods := &installedMods{}
	mgr := NewInstanceManager(
		store.NewInstanceRepo(st), state, nil, 64, 4, 4,
		"manifests/minecraft-modded", "192.168.20.224",
		manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded"), "minecraft-modded",
		WithModsReader(func(context.Context, int) ([]string, error) { return mods.lines, nil }),
	)
	return mgr, state, mods, context.Background()
}

func seedWorld(t *testing.T, mgr *InstanceManager, ctx context.Context, mods string) domain.Instance {
	t.Helper()
	_ = mods
	inst, err := mgr.CreateInstance(ctx, domain.Instance{
		Name:   "Fluxweave",
		Source: domain.SourceModlist,
		Tier:   domain.TierMedium,
		Minecraft: &domain.MinecraftConfig{
			Loader: domain.LoaderNeoForge, MCVersion: "1.21.1",
		},
	}, domain.ModList{Primary: mods})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	return *inst
}

// Before this, settings changes only touched SQLite and never reached git, so a
// resize was invisible to the cluster. SetResources must rewrite the manifest.
func TestSetResourcesRewritesTheManifest(t *testing.T) {
	mgr, state, installed, ctx := newResizeManager(t)
	_ = installed
	inst := seedWorld(t, mgr, ctx, "")

	dep := func() string {
		return string(state.docs["manifests/minecraft-modded/instance-01/deployment.yaml"].Raw)
	}
	if !strings.Contains(dep(), "memory: 8Gi") {
		t.Fatalf("expected the medium tier's 8Gi at creation, got:\n%s", dep())
	}

	if err := mgr.SetResources(ctx, inst.Number, domain.Resources{
		MemRequestGiB: 24, MemLimitGiB: 32, CPURequestMilli: 4000,
	}, "me"); err != nil {
		t.Fatalf("SetResources: %v", err)
	}

	got := dep()
	for _, want := range []string{"memory: 24Gi", "memory: 32Gi", "cpu: 4000m"} {
		if !strings.Contains(got, want) {
			t.Errorf("resize did not reach the manifest: missing %q", want)
		}
	}
	if strings.Contains(got, "memory: 8Gi") {
		t.Error("the old tier size is still in the manifest")
	}
}

// Re-rendering uses the whole template, so a resize that forgot to pass the
// world's current mods would silently empty mods.yaml and strip every mod.
func TestSetResourcesPreservesMods(t *testing.T) {
	mgr, state, installed, ctx := newResizeManager(t)
	installed.lines = []string{"fabric/sodium", "fabric/lithium"}
	inst := seedWorld(t, mgr, ctx, "fabric/sodium\nfabric/lithium\n")

	modsPath := "manifests/minecraft-modded/instance-01/mods.yaml"
	before := string(state.docs[modsPath].Raw)
	if !strings.Contains(before, "sodium") {
		t.Fatalf("seed failed, mods.yaml has no mods:\n%s", before)
	}

	if err := mgr.SetResources(ctx, inst.Number,
		domain.Resources{MemRequestGiB: 12, MemLimitGiB: 16}, "me"); err != nil {
		t.Fatal(err)
	}

	after := string(state.docs[modsPath].Raw)
	for _, mod := range []string{"sodium", "lithium"} {
		if !strings.Contains(after, mod) {
			t.Errorf("resize stripped %q from mods.yaml:\n%s", mod, after)
		}
	}
}

func TestSetResourcesPersistsToTheRecord(t *testing.T) {
	mgr, _, installed, ctx := newResizeManager(t)
	_ = installed
	inst := seedWorld(t, mgr, ctx, "")

	want := domain.Resources{MemRequestGiB: 12, MemLimitGiB: 16, CPURequestMilli: 3000}
	if err := mgr.SetResources(ctx, inst.Number, want, "me"); err != nil {
		t.Fatal(err)
	}

	got, err := mgr.GetInstance(ctx, inst.Number)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.Resources != want {
		t.Errorf("stored resources = %+v, want %+v", got.Resources, want)
	}
	if !got.DriftsFromTier(domain.MinecraftProfile) {
		t.Error("a world sized away from its tier must report drift")
	}
}

func TestSetResourcesRejectsNonsense(t *testing.T) {
	mgr, state, installed, ctx := newResizeManager(t)
	_ = installed
	inst := seedWorld(t, mgr, ctx, "")
	before := string(state.docs["manifests/minecraft-modded/instance-01/deployment.yaml"].Raw)

	if err := mgr.SetResources(ctx, inst.Number,
		domain.Resources{MemRequestGiB: 16, MemLimitGiB: 4}, "me"); err == nil {
		t.Error("a limit below the request must be rejected")
	}
	if after := string(state.docs["manifests/minecraft-modded/instance-01/deployment.yaml"].Raw); after != before {
		t.Error("a rejected resize must not touch the manifest")
	}
}

func TestApplyTierMovesWorldBackOntoTier(t *testing.T) {
	mgr, _, installed, ctx := newResizeManager(t)
	_ = installed
	inst := seedWorld(t, mgr, ctx, "")

	if err := mgr.SetResources(ctx, inst.Number,
		domain.Resources{MemRequestGiB: 24, MemLimitGiB: 32}, "me"); err != nil {
		t.Fatal(err)
	}

	tier := domain.Tier{
		Key: "large", GameID: domain.GameMinecraft,
		Resources: domain.Resources{MemRequestGiB: 12, MemLimitGiB: 16, CPURequestMilli: 2000},
	}
	if err := mgr.ApplyTier(ctx, inst.Number, tier, "me"); err != nil {
		t.Fatalf("ApplyTier: %v", err)
	}

	got, err := mgr.GetInstance(ctx, inst.Number)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.Resources != tier.Resources {
		t.Errorf("resources = %+v, want %+v", got.Resources, tier.Resources)
	}
	if got.Tier != domain.TierLarge {
		t.Errorf("tier = %q, want large", got.Tier)
	}
	if got.DriftsFromTier(domain.MinecraftProfile) {
		t.Error("a world re-applied onto its tier must no longer drift")
	}
}
