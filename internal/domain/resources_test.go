package domain

import "testing"

// The values below are the sizes every existing world was running on before
// resources became per-instance data. LegacyResources is the single source for
// both the EffectiveResources fallback and the storage backfill, so changing a
// number here resizes live worlds on the next deploy. That is the point of this
// test: make that impossible to do by accident.
func TestLegacyResourcesMatchHistoricalSizes(t *testing.T) {
	cases := []struct {
		game    GameID
		tier    ResourceTier
		request int
		limit   int
		heap    int
	}{
		{GameMinecraft, TierSmall, 4, 6, 3},
		{GameMinecraft, TierMedium, 8, 10, 6},
		{GameMinecraft, TierLarge, 12, 16, 10},
		{GameValheim, TierSmall, 4, 5, 0},
		{GameValheim, TierMedium, 6, 7, 0},
		{GameValheim, TierLarge, 8, 10, 0},
	}

	for _, c := range cases {
		got := LegacyResources(c.game, c.tier)
		if got.MemRequestGiB != c.request || got.MemLimitGiB != c.limit {
			t.Errorf("%s/%s resources = %d/%d GiB, want %d/%d",
				c.game, c.tier, got.MemRequestGiB, got.MemLimitGiB, c.request, c.limit)
		}
		// Both deployment templates have always hardcoded `cpu: 2000m` with no
		// limit. The legacy table must say the same or the request vanishes from
		// every rendered manifest and every world restarts.
		if got.CPURequestMilli != 2000 || got.CPULimitMilli != 0 {
			t.Errorf("%s/%s cpu = %d/%d milli, want 2000/0",
				c.game, c.tier, got.CPURequestMilli, got.CPULimitMilli)
		}
		if h := LegacyHeapInitGiB(c.game, c.tier); h != c.heap {
			t.Errorf("%s/%s heap = %d, want %d", c.game, c.tier, h, c.heap)
		}
	}
}

func TestEffectiveResourcesPrefersExplicitOverTier(t *testing.T) {
	inst := Instance{Number: 1, Tier: TierSmall, Resources: Resources{MemRequestGiB: 16, MemLimitGiB: 20}}
	got := inst.EffectiveResources(ValheimProfile)
	if got.MemRequestGiB != 16 || got.MemLimitGiB != 20 {
		t.Errorf("explicit resources must win over the tier, got %+v", got)
	}
	if !inst.DriftsFromTier(ValheimProfile) {
		t.Error("an instance sized away from its tier must report drift")
	}
}

func TestEffectiveResourcesFallsBackToTier(t *testing.T) {
	inst := Instance{Number: 1, Tier: TierLarge}
	got := inst.EffectiveResources(MinecraftProfile)
	if got.MemRequestGiB != 12 || got.MemLimitGiB != 16 {
		t.Errorf("an instance with no stored resources must keep its historical size, got %+v", got)
	}
	if inst.DriftsFromTier(MinecraftProfile) {
		t.Error("an instance on its tier's sizes must not report drift")
	}
}

func TestResourcesValidate(t *testing.T) {
	bad := []Resources{
		{MemRequestGiB: 0, MemLimitGiB: 4},
		{MemRequestGiB: 8, MemLimitGiB: 4},
		{MemRequestGiB: 4, MemLimitGiB: 8, CPURequestMilli: 2000, CPULimitMilli: 1000},
		{MemRequestGiB: 4, MemLimitGiB: 8, CPURequestMilli: -1},
	}
	for _, r := range bad {
		if err := r.Validate(); err == nil {
			t.Errorf("expected %+v to be rejected", r)
		}
	}
	if err := (Resources{MemRequestGiB: 4, MemLimitGiB: 8, CPURequestMilli: 500, CPULimitMilli: 2000}).Validate(); err != nil {
		t.Errorf("valid resources rejected: %v", err)
	}
}

func TestResourcesFitsCeiling(t *testing.T) {
	ceiling := Resources{MemLimitGiB: 8}
	if !(Resources{MemRequestGiB: 4, MemLimitGiB: 8}).Fits(ceiling) {
		t.Error("resources at the ceiling must fit")
	}
	if (Resources{MemRequestGiB: 4, MemLimitGiB: 9}).Fits(ceiling) {
		t.Error("resources above the ceiling must not fit")
	}
	if !(Resources{MemRequestGiB: 99, MemLimitGiB: 99}).Fits(Resources{}) {
		t.Error("a zero ceiling means unbounded")
	}
}
