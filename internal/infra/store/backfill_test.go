package store

import (
	"path/filepath"
	"testing"

	"agrelha/internal/domain"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "backfill.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// A world that existed before resources became per-instance data must come out
// of the migration running exactly the size it ran at before. Anything else
// resizes live worlds on deploy.
func TestBackfillReproducesHistoricalSizes(t *testing.T) {
	st := openTemp(t)

	cases := []struct {
		game    domain.GameID
		tier    domain.ResourceTier
		request int
		limit   int
		heap    int
	}{
		{domain.GameMinecraft, domain.TierSmall, 4, 6, 3},
		{domain.GameMinecraft, domain.TierMedium, 8, 10, 6},
		{domain.GameMinecraft, domain.TierLarge, 12, 16, 10},
		{domain.GameValheim, domain.TierSmall, 4, 5, 0},
		{domain.GameValheim, domain.TierMedium, 6, 7, 0},
		{domain.GameValheim, domain.TierLarge, 8, 10, 0},
	}

	num := 0
	for _, c := range cases {
		num++
		profile, _ := domain.ProfileFor(c.game)
		rec := InstanceRecord{Number: num, Name: "w", Slug: "w", Tier: string(c.tier), State: "stopped"}
		var err error
		if c.game == domain.GameValheim {
			err = st.UpsertValheimInstance(rec)
		} else {
			err = st.UpsertInstance(rec)
		}
		if err != nil {
			t.Fatalf("seed %s/%s: %v", c.game, c.tier, err)
		}

		if _, err := st.backfillTableResources(profile); err != nil {
			t.Fatalf("backfill %s: %v", c.game, err)
		}

		var got *InstanceRecord
		if c.game == domain.GameValheim {
			got, err = st.GetValheimInstance(num)
		} else {
			got, err = st.GetInstance(num)
		}
		if err != nil || got == nil {
			t.Fatalf("read back %s/%s: %v", c.game, c.tier, err)
		}
		if got.MemRequestGiB != c.request || got.MemLimitGiB != c.limit {
			t.Errorf("%s/%s backfilled to %d/%d GiB, want %d/%d",
				c.game, c.tier, got.MemRequestGiB, got.MemLimitGiB, c.request, c.limit)
		}
		if got.CPURequestMilli != 2000 || got.CPULimitMilli != 0 {
			t.Errorf("%s/%s backfill must preserve the historical 2000m cpu request, got %d/%d",
				c.game, c.tier, got.CPURequestMilli, got.CPULimitMilli)
		}
		if c.game == domain.GameMinecraft && got.HeapInitGiB != c.heap {
			t.Errorf("%s/%s heap = %d, want %d", c.game, c.tier, got.HeapInitGiB, c.heap)
		}
	}
}

func TestBackfillLeavesExplicitResourcesAlone(t *testing.T) {
	st := openTemp(t)
	profile, _ := domain.ProfileFor(domain.GameValheim)

	if err := st.UpsertValheimInstance(InstanceRecord{
		Number: 1, Name: "boppo", Slug: "boppo", Tier: string(domain.TierSmall), State: "stopped",
		MemRequestGiB: 16, MemLimitGiB: 20,
	}); err != nil {
		t.Fatal(err)
	}

	for range 3 {
		if _, err := st.backfillTableResources(profile); err != nil {
			t.Fatal(err)
		}
	}

	got, err := st.GetValheimInstance(1)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.MemRequestGiB != 16 || got.MemLimitGiB != 20 {
		t.Errorf("backfill overwrote hand-set resources: got %d/%d, want 16/20",
			got.MemRequestGiB, got.MemLimitGiB)
	}
}

func TestBackfillIsIdempotent(t *testing.T) {
	st := openTemp(t)
	profile, _ := domain.ProfileFor(domain.GameMinecraft)

	if err := st.UpsertInstance(InstanceRecord{
		Number: 1, Name: "w", Slug: "w", Tier: string(domain.TierLarge), State: "stopped",
	}); err != nil {
		t.Fatal(err)
	}

	first, err := st.backfillTableResources(profile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.backfillTableResources(profile)
	if err != nil {
		t.Fatal(err)
	}
	if first != 1 || second != 0 {
		t.Errorf("backfill should touch 1 row then 0, got %d then %d", first, second)
	}
}
