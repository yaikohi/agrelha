package capacity_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"agrelha/internal/app/capacity"
	"agrelha/internal/domain"
	"agrelha/internal/infra/store"
)

func newService(t *testing.T) (*capacity.Service, context.Context) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "capacity.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return capacity.New(st, st, capacity.WithGlobals(st)), context.Background()
}

func TestSeedCreatesCatalogueFromHistoricalSizes(t *testing.T) {
	svc, ctx := newService(t)
	if err := svc.Seed(ctx, domain.GameValheim, domain.GameSettings{TotalBudgetGiB: 16}); err != nil {
		t.Fatal(err)
	}

	tiers, err := svc.Tiers(ctx, domain.GameValheim)
	if err != nil {
		t.Fatal(err)
	}
	if len(tiers) != 3 {
		t.Fatalf("expected 3 seeded tiers, got %d", len(tiers))
	}
	// Seeded sizes must be the sizes worlds already run at.
	want := map[string][2]int{"small": {4, 5}, "medium": {6, 7}, "large": {8, 10}}
	for _, tier := range tiers {
		w, ok := want[tier.Key]
		if !ok {
			t.Errorf("unexpected tier %q", tier.Key)
			continue
		}
		if tier.Resources.MemRequestGiB != w[0] || tier.Resources.MemLimitGiB != w[1] {
			t.Errorf("tier %s = %d/%d GiB, want %d/%d", tier.Key,
				tier.Resources.MemRequestGiB, tier.Resources.MemLimitGiB, w[0], w[1])
		}
	}
}

func TestSeedNeverOverwritesOperatorEdits(t *testing.T) {
	svc, ctx := newService(t)
	if err := svc.Seed(ctx, domain.GameValheim, domain.GameSettings{TotalBudgetGiB: 16}); err != nil {
		t.Fatal(err)
	}
	edited := domain.Tier{
		Key: "large", GameID: domain.GameValheim, Name: "Large",
		Resources: domain.Resources{MemRequestGiB: 16, MemLimitGiB: 20, CPURequestMilli: 2000},
	}
	if err := svc.PutTier(ctx, edited, "me"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Seed(ctx, domain.GameValheim, domain.GameSettings{TotalBudgetGiB: 99}); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Tier(ctx, domain.GameValheim, "large")
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.Resources.MemLimitGiB != 20 {
		t.Errorf("re-seeding clobbered an edited tier: limit = %d, want 20", got.Resources.MemLimitGiB)
	}
	s, err := svc.Settings(ctx, domain.GameValheim)
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalBudgetGiB != 16 {
		t.Errorf("re-seeding clobbered settings: budget = %d, want 16", s.TotalBudgetGiB)
	}
}

func TestCeilingBindsNonAdminsOnly(t *testing.T) {
	svc, ctx := newService(t)
	if err := svc.Seed(ctx, domain.GameValheim, domain.GameSettings{
		TotalBudgetGiB: 16,
		Ceiling:        domain.Resources{MemLimitGiB: 10},
	}); err != nil {
		t.Fatal(err)
	}

	big := domain.Resources{MemRequestGiB: 16, MemLimitGiB: 20, CPURequestMilli: 2000}

	if err := svc.CheckResources(ctx, domain.GameValheim, big, false); !errors.Is(err, capacity.ErrAboveCeiling) {
		t.Errorf("non-admin above the ceiling should be refused, got %v", err)
	}
	if err := svc.CheckResources(ctx, domain.GameValheim, big, true); err != nil {
		t.Errorf("admin must not be bound by the ceiling, got %v", err)
	}

	ok := domain.Resources{MemRequestGiB: 8, MemLimitGiB: 10, CPURequestMilli: 2000}
	if err := svc.CheckResources(ctx, domain.GameValheim, ok, false); err != nil {
		t.Errorf("non-admin at the ceiling should be allowed, got %v", err)
	}
}

func TestCheckResourcesRejectsNonsenseEvenForAdmins(t *testing.T) {
	svc, ctx := newService(t)
	bad := domain.Resources{MemRequestGiB: 8, MemLimitGiB: 4}
	if err := svc.CheckResources(ctx, domain.GameValheim, bad, true); err == nil {
		t.Error("a limit below the request must be rejected regardless of who asks")
	}
}

func TestPutTierRejectsInvalidResources(t *testing.T) {
	svc, ctx := newService(t)
	bad := domain.Tier{Key: "broken", GameID: domain.GameValheim,
		Resources: domain.Resources{MemRequestGiB: 0, MemLimitGiB: 4}}
	if err := svc.PutTier(ctx, bad, "me"); err == nil {
		t.Error("a tier with no memory request must be rejected")
	}
}

func TestFreeCreationsDefaultsToOne(t *testing.T) {
	svc, ctx := newService(t)
	if n := svc.FreeCreations(ctx); n != capacity.DefaultFreeCreations {
		t.Errorf("unset allowance = %d, want %d", n, capacity.DefaultFreeCreations)
	}
}

func TestFreeCreationsPersistsAndValidates(t *testing.T) {
	svc, ctx := newService(t)

	if err := svc.SetFreeCreations(ctx, 3, "me"); err != nil {
		t.Fatal(err)
	}
	if n := svc.FreeCreations(ctx); n != 3 {
		t.Errorf("allowance = %d, want 3", n)
	}

	// Zero is meaningful: every world then needs approval.
	if err := svc.SetFreeCreations(ctx, 0, "me"); err != nil {
		t.Fatal(err)
	}
	if n := svc.FreeCreations(ctx); n != 0 {
		t.Errorf("allowance = %d, want 0", n)
	}

	if err := svc.SetFreeCreations(ctx, -1, "me"); err == nil {
		t.Error("a negative allowance must be rejected")
	}
}
