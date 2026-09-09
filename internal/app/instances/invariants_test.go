package instances

import (
	"agrelha/internal/domain"
	"agrelha/internal/infra/manifests"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"agrelha/internal/infra/store"
)

func TestResourceTiersMemoryMapping(t *testing.T) {
	cases := []struct {
		tier         domain.ResourceTier
		wantMem      int
		wantLimit    int
		wantHeapInit int
	}{
		{domain.TierSmall, 4, 6, 3},
		{domain.TierMedium, 8, 10, 6},
		{domain.TierLarge, 12, 16, 10},
		{domain.ResourceTier("unknown"), 8, 10, 6}, // fallback to medium
	}

	for _, tc := range cases {
		inst := domain.Instance{Tier: tc.tier}
		if got := inst.MemoryGiB(); got != tc.wantMem {
			t.Errorf("tier %s MemoryGiB = %d, want %d", tc.tier, got, tc.wantMem)
		}
		if got := inst.MemoryLimitGiB(); got != tc.wantLimit {
			t.Errorf("tier %s MemoryLimitGiB = %d, want %d", tc.tier, got, tc.wantLimit)
		}
		if got := inst.HeapInitMemoryGiB(); got != tc.wantHeapInit {
			t.Errorf("tier %s HeapInitMemoryGiB = %d, want %d", tc.tier, got, tc.wantHeapInit)
		}
	}

	// NormalizeTier checks
	if got := domain.NormalizeTier("small"); got != domain.TierSmall {
		t.Errorf("NormalizeTier(small) = %s, want %s", got, domain.TierSmall)
	}
	if got := domain.NormalizeTier("  LARGE  "); got != domain.TierLarge {
		t.Errorf("NormalizeTier(LARGE) = %s, want %s", got, domain.TierLarge)
	}
	if got := domain.NormalizeTier("custom"); got != domain.TierMedium {
		t.Errorf("NormalizeTier(custom) = %s, want %s", got, domain.TierMedium)
	}
}

func TestEnvDisjointKeySetsPerSource(t *testing.T) {
	t.Run("curseforge_pack", func(t *testing.T) {
		inst := domain.Instance{
			Number: 1, Name: "CF domain.Pack", Slug: "cf-pack",
			Source: domain.SourceModpack,
			Pack:   &domain.Pack{Provider: domain.ProviderCurseForge, Ref: "https://curseforge.com/pack", Name: "ATM9"},
		}
		env := inst.Env()
		if env["TYPE"] != "AUTO_CURSEFORGE" {
			t.Fatalf("expected TYPE=AUTO_CURSEFORGE, got %q", env["TYPE"])
		}
		if env["CF_PAGE_URL"] != "https://curseforge.com/pack" {
			t.Fatalf("expected CF_PAGE_URL to be set, got %q", env["CF_PAGE_URL"])
		}
		if _, ok := env["MODRINTH_MODPACK"]; ok {
			t.Fatal("CurseForge pack must not set MODRINTH_MODPACK")
		}
		if _, ok := env["MODRINTH_PROJECTS"]; ok {
			t.Fatal("CurseForge pack must not set MODRINTH_PROJECTS")
		}
	})

	t.Run("modrinth_pack", func(t *testing.T) {
		inst := domain.Instance{
			Number: 2, Name: "MR domain.Pack", Slug: "mr-pack",
			Source: domain.SourceModpack,
			Pack:   &domain.Pack{Provider: domain.ProviderModrinth, Ref: "mr-pack-slug", Name: "Fabulously Optimized"},
		}
		env := inst.Env()
		if env["TYPE"] != "MODRINTH" {
			t.Fatalf("expected TYPE=MODRINTH, got %q", env["TYPE"])
		}
		if env["MODRINTH_MODPACK"] != "mr-pack-slug" {
			t.Fatalf("expected MODRINTH_MODPACK to be set, got %q", env["MODRINTH_MODPACK"])
		}
		if _, ok := env["CF_PAGE_URL"]; ok {
			t.Fatal("Modrinth pack must not set CF_PAGE_URL")
		}
		if _, ok := env["MODRINTH_PROJECTS"]; ok {
			t.Fatal("Modrinth pack must not set MODRINTH_PROJECTS")
		}
	})

	t.Run("vanilla", func(t *testing.T) {
		inst := domain.Instance{
			Number: 3, Name: "Vanilla", Slug: "vanilla",
			Source: domain.SourceVanilla, MCVersion: "1.21.4",
		}
		env := inst.Env()
		if env["TYPE"] != "VANILLA" {
			t.Fatalf("expected TYPE=VANILLA, got %q", env["TYPE"])
		}
		if env["VERSION"] != "1.21.4" {
			t.Fatalf("expected VERSION=1.21.4, got %q", env["VERSION"])
		}
		if _, ok := env["CF_PAGE_URL"]; ok {
			t.Fatal("Vanilla must not set CF_PAGE_URL")
		}
		if _, ok := env["MODRINTH_MODPACK"]; ok {
			t.Fatal("Vanilla must not set MODRINTH_MODPACK")
		}
		if _, ok := env["MODRINTH_PROJECTS"]; ok {
			t.Fatal("Vanilla must not set MODRINTH_PROJECTS")
		}
		if _, ok := env["FABRIC_LOADER_VERSION"]; ok {
			t.Fatal("Vanilla must not set FABRIC_LOADER_VERSION")
		}
		if _, ok := env["NEOFORGE_VERSION"]; ok {
			t.Fatal("Vanilla must not set NEOFORGE_VERSION")
		}
	})

	t.Run("modlist_fabric", func(t *testing.T) {
		inst := domain.Instance{
			Number: 4, Name: "Fabric Modded", Slug: "fabric-modded",
			Source: domain.SourceModlist, Loader: domain.LoaderFabric, MCVersion: "1.21.1",
		}
		env := inst.Env()
		if env["TYPE"] != "FABRIC" {
			t.Fatalf("expected TYPE=FABRIC, got %q", env["TYPE"])
		}
		if env["FABRIC_LOADER_VERSION"] != "latest" {
			t.Fatalf("expected FABRIC_LOADER_VERSION=latest, got %q", env["FABRIC_LOADER_VERSION"])
		}
		if _, ok := env["NEOFORGE_VERSION"]; ok {
			t.Fatal("Fabric instance must not set NEOFORGE_VERSION")
		}
		if env["MODRINTH_PROJECTS"] != "@/config-mods/mods.txt" {
			t.Fatalf("expected MODRINTH_PROJECTS=@/config-mods/mods.txt, got %q", env["MODRINTH_PROJECTS"])
		}
	})

	t.Run("modlist_neoforge", func(t *testing.T) {
		inst := domain.Instance{
			Number: 5, Name: "NeoForge Modded", Slug: "neoforge-modded",
			Source: domain.SourceModlist, Loader: domain.LoaderNeoForge, MCVersion: "1.21.1",
		}
		env := inst.Env()
		if env["TYPE"] != "NEOFORGE" {
			t.Fatalf("expected TYPE=NEOFORGE, got %q", env["TYPE"])
		}
		if env["NEOFORGE_VERSION"] != "latest" {
			t.Fatalf("expected NEOFORGE_VERSION=latest, got %q", env["NEOFORGE_VERSION"])
		}
		if _, ok := env["FABRIC_LOADER_VERSION"]; ok {
			t.Fatal("NeoForge instance must not set FABRIC_LOADER_VERSION")
		}
		if env["MODRINTH_PROJECTS"] != "@/config-mods/mods.txt" {
			t.Fatalf("expected MODRINTH_PROJECTS=@/config-mods/mods.txt, got %q", env["MODRINTH_PROJECTS"])
		}
	})
}

func TestBudgetRejectsRAMOvercommit(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	defer st.Close()

	// Budget configured for 16 GiB total, allowing up to 3 running instances
	mgr := NewInstanceManager(
		store.NewInstanceRepo(st), nil, nil, 16, 4, 3, "manifests/mc", "", manifests.New("", "minecraft-modded"), "minecraft-modded")
	ctx := context.Background()

	// Instance 1: Large (12 GiB)
	_, err = mgr.CreateInstance(ctx, domain.Instance{
		Name:      "Heavy Server",
		Source:    domain.SourceVanilla,
		MCVersion: "1.21.4",
		Tier:      domain.TierLarge, // 12 GiB
	}, "")
	if err != nil {
		t.Fatalf("create inst1: %v", err)
	}

	// Instance 2: Medium (8 GiB)
	_, err = mgr.CreateInstance(ctx, domain.Instance{
		Name:      "Medium Server",
		Source:    domain.SourceVanilla,
		MCVersion: "1.21.4",
		Tier:      domain.TierMedium, // 8 GiB
	}, "")
	if err != nil {
		t.Fatalf("create inst2: %v", err)
	}

	// Start Instance 1 (12 GiB <= 16 GiB) -> succeeds
	if err := mgr.StartInstance(ctx, 1); err != nil {
		t.Fatalf("start inst1 failed: %v", err)
	}

	// Start Instance 2 (12 + 8 = 20 GiB > 16 GiB total budget) -> must FAIL with RAM budget error
	err = mgr.StartInstance(ctx, 2)
	if err == nil {
		t.Fatalf("expected RAM budget overcommit error, but StartInstance succeeded")
	}
	if !strings.Contains(err.Error(), "RAM budget exceeded") {
		t.Fatalf("expected 'RAM budget exceeded' error message, got: %v", err)
	}
}

func TestMaxInstancesLimit(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	defer st.Close()

	// Max instances = 2
	mgr := NewInstanceManager(
		store.NewInstanceRepo(st), nil, nil, 24, 2, 2, "manifests/mc", "", manifests.New("", "minecraft-modded"), "minecraft-modded")
	ctx := context.Background()

	_, err = mgr.CreateInstance(ctx, domain.Instance{Name: "One", Source: domain.SourceVanilla, MCVersion: "1.21.4"}, "")
	if err != nil {
		t.Fatalf("create One: %v", err)
	}
	_, err = mgr.CreateInstance(ctx, domain.Instance{Name: "Two", Source: domain.SourceVanilla, MCVersion: "1.21.4"}, "")
	if err != nil {
		t.Fatalf("create Two: %v", err)
	}

	// Third creation must be rejected
	_, err = mgr.CreateInstance(ctx, domain.Instance{Name: "Three", Source: domain.SourceVanilla, MCVersion: "1.21.4"}, "")
	if err == nil {
		t.Fatalf("expected error when exceeding max instances limit of 2, got nil")
	}
	if !strings.Contains(err.Error(), "maximum limit of 2 instances reached") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
