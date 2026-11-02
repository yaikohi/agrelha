package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

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

	instanceNumber := 0
	for _, c := range cases {
		instanceNumber++
		profile, _ := domain.ProfileFor(c.game)
		rec := InstanceRecord{Number: instanceNumber, Name: "w", Slug: "w", Tier: string(c.tier), State: "stopped"}
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
			got, err = st.GetValheimInstance(instanceNumber)
		} else {
			got, err = st.GetInstance(instanceNumber)
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

func TestBackfillWorldIDsAndSweeps(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()

	// Seed instance in slot 1 with empty world_id and a fixed created_at in the past
	occupantCreated := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	if _, err := st.db.Exec(`
		INSERT INTO mc_instances (number, world_id, name, slug, tier, state, created_at, last_used)
		VALUES (1, '', 'OldWorld', 'oldworld', 'medium', 'stopped', ?, ?)`,
		occupantCreated.Format("2006-01-02 15:04:05"), occupantCreated.Format("2006-01-02 15:04:05")); err != nil {
		t.Fatalf("seed instance: %v", err)
	}

	// Incident 1: 2 hours before occupantCreated (from a prior deleted occupant of slot 1)
	priorAt := occupantCreated.Add(-2 * time.Hour)
	if _, err := st.RecordIncident(ctx, domain.Incident{
		GameID: domain.GameMinecraft, Number: 1, At: priorAt, Reason: "OldCrash",
	}); err != nil {
		t.Fatalf("record prior incident: %v", err)
	}

	// Incident 2: 2 hours after occupantCreated (belongs to the current occupant)
	currentAt := occupantCreated.Add(2 * time.Hour)
	if _, err := st.RecordIncident(ctx, domain.Incident{
		GameID: domain.GameMinecraft, Number: 1, At: currentAt, Reason: "CurrentCrash",
	}); err != nil {
		t.Fatalf("record current incident: %v", err)
	}

	// Ephemeral data: active restore point for slot 1, orphaned restore point for slot 2
	if err := st.SaveRestorePoint(domain.GameMinecraft, 1, []string{"prev1"}, []string{"appl1"}); err != nil {
		t.Fatalf("save restore point 1: %v", err)
	}
	if err := st.SaveRestorePoint(domain.GameMinecraft, 2, []string{"prev2"}, []string{"appl2"}); err != nil {
		t.Fatalf("save restore point 2: %v", err)
	}

	// Ephemeral data: presence for active Valheim slot 1 and presence for orphaned slot 99
	if _, err := st.db.Exec(`
		INSERT INTO valheim_instances (number, world_id, name, slug, tier, state)
		VALUES (1, '', 'Val1', 'val1', 'medium', 'stopped')`); err != nil {
		t.Fatalf("seed valheim instance: %v", err)
	}
	if err := st.SetOnline(1, "steam-1", true); err != nil {
		t.Fatalf("set online slot 1: %v", err)
	}
	if err := st.SetOnline(99, "steam-2", true); err != nil {
		t.Fatalf("set online slot 99: %v", err)
	}

	// Execute backfill
	if err := st.backfillWorldIDs(); err != nil {
		t.Fatalf("backfillWorldIDs: %v", err)
	}

	// Check mc_instances slot 1 world_id
	inst, err := st.GetInstance(1)
	if err != nil || inst == nil {
		t.Fatalf("get instance: %v", err)
	}
	if inst.WorldID == "" {
		t.Fatalf("instance world_id was not backfilled")
	}

	// Verify incidents:
	// Only the incident at currentAt should belong to inst.WorldID
	incidents, err := st.ListIncidents(ctx, inst.WorldID, 10, false)
	if err != nil {
		t.Fatalf("list incidents for backfilled world: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("expected 1 incident for backfilled world, got %d", len(incidents))
	}
	if incidents[0].Reason != "CurrentCrash" {
		t.Errorf("expected CurrentCrash, got %s", incidents[0].Reason)
	}

	// Prior incident must stay orphaned (empty world_id)
	var priorCount int
	if err := st.db.QueryRow(`SELECT count(*) FROM incidents WHERE COALESCE(world_id, '') = '' AND reason = 'OldCrash'`).Scan(&priorCount); err != nil || priorCount != 1 {
		t.Errorf("prior incident should remain orphaned, got count=%d, err=%v", priorCount, err)
	}

	// Verify restore points: slot 1 preserved, orphaned slot 2 removed
	rp1, err := st.RestorePoint(domain.GameMinecraft, 1)
	if err != nil || rp1 == nil {
		t.Errorf("active restore point for slot 1 should survive: %v", err)
	}
	rp2, err := st.RestorePoint(domain.GameMinecraft, 2)
	if err != nil || rp2 != nil {
		t.Errorf("orphaned restore point for slot 2 should be wiped: %v", rp2)
	}

	// Verify presence: slot 1 preserved, orphaned slot 99 wiped
	p1, err := st.CountOnlineForInstance(1)
	if err != nil || p1 != 1 {
		t.Errorf("active presence for slot 1 should survive, got count=%d", p1)
	}
	p99, err := st.CountOnlineForInstance(99)
	if err != nil || p99 != 0 {
		t.Errorf("orphaned presence for slot 99 should be wiped, got count=%d", p99)
	}

	// Verify idempotency: running backfill again leaves instance world_id untouched
	if err := st.backfillWorldIDs(); err != nil {
		t.Fatalf("second backfillWorldIDs: %v", err)
	}
	inst2, err := st.GetInstance(1)
	if err != nil || inst2 == nil || inst2.WorldID != inst.WorldID {
		t.Fatalf("world_id changed on second backfill: was %q, now %v", inst.WorldID, inst2)
	}
}
