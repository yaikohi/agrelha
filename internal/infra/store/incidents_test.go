package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"agrelha/internal/domain"
)

func TestIncidentRoundTrip(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "inc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()

	wid := domain.NewID()
	if got, err := st.LastIncident(ctx, wid); err != nil || got != nil {
		t.Fatalf("no incidents yet: got %v, %v", got, err)
	}

	older := domain.Incident{WorldID: wid, GameID: domain.GameMinecraft, Number: 3, At: time.Now().Add(-time.Hour), ExitCode: 1, Reason: "Error"}
	newer := domain.Incident{
		WorldID: wid, GameID: domain.GameMinecraft, Number: 3, At: time.Now(),
		RestartCount: 10, Reason: "CrashLoopBackOff", OOMKilled: true,
		LogTail: "java.lang.OutOfMemoryError",
	}
	for _, in := range []domain.Incident{older, newer} {
		if _, err := st.RecordIncident(ctx, in); err != nil {
			t.Fatalf("RecordIncident: %v", err)
		}
	}

	got, err := st.LastIncident(ctx, wid)
	if err != nil || got == nil {
		t.Fatalf("LastIncident: %v, %v", got, err)
	}
	if !got.OOMKilled || got.RestartCount != 10 {
		t.Errorf("LastIncident returned the older row: %+v", got)
	}
	if got.LogTail == "" {
		t.Error("log tail must survive: it is the only record of why the server died")
	}

	if all, err := st.ListIncidents(ctx, wid, 10, false); err != nil || len(all) != 2 {
		t.Errorf("ListIncidents = %d rows, %v", len(all), err)
	}
	otherWID := domain.NewID()
	if other, err := st.ListIncidents(ctx, otherWID, 10, false); err != nil || len(other) != 0 {
		t.Errorf("incidents must be scoped per world: got %d", len(other))
	}

	// Test RecordIncident with zero time and ListIncidents with limit <= 0
	id, err := st.RecordIncident(ctx, domain.Incident{
		WorldID: wid,
		GameID:  domain.GameMinecraft,
		Number:  3,
		Reason:  "SyntheticZeroTime",
	})
	if err != nil || id == 0 {
		t.Fatalf("RecordIncident with zero time failed: %v", err)
	}
	defaultLimitList, err := st.ListIncidents(ctx, wid, 0, false)
	if err != nil || len(defaultLimitList) != 3 {
		t.Fatalf("ListIncidents with limit 0 should default to 20, got count %d, err=%v", len(defaultLimitList), err)
	}
}

func TestIncidentSummary(t *testing.T) {
	cases := []struct {
		in   domain.Incident
		want string
	}{
		{domain.Incident{OOMKilled: true}, "Out of memory"},
		{domain.Incident{Reason: "CrashLoopBackOff", RestartCount: 7}, "restarted 7 times"},
		{domain.Incident{ExitCode: 137}, "code 137"},
		{domain.Incident{}, "Stopped unexpectedly"},
	}
	for _, c := range cases {
		if got := c.in.Summary(); got == "" || !contains(got, c.want) {
			t.Errorf("Summary(%+v) = %q, want it to mention %q", c.in, got, c.want)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})())
}

func TestRecycledSlotIncidentIsolation(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "recycle.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	// World 1 in slot 1
	w1ID := domain.NewID()
	if err := st.UpsertInstance(InstanceRecord{
		Number: 1, WorldID: w1ID, Name: "World 1", Slug: "world-1", Tier: "medium", State: "stopped",
	}); err != nil {
		t.Fatal(err)
	}

	// World 1 records crashes
	for i := 1; i <= 2; i++ {
		if _, err := st.RecordIncident(ctx, domain.Incident{
			WorldID: w1ID, GameID: domain.GameMinecraft, Number: 1,
			At: time.Now().Add(time.Duration(i) * time.Minute),
			ExitCode: 1, Reason: "World 1 Died",
		}); err != nil {
			t.Fatal(err)
		}
	}

	// World 1 sees its crashes
	w1Crashes, err := st.ListIncidents(ctx, w1ID, 10, false)
	if err != nil || len(w1Crashes) != 2 {
		t.Fatalf("world 1 should see 2 crashes, got %d, err=%v", len(w1Crashes), err)
	}

	// World 1 is deleted from slot 1
	if err := st.DeleteInstance(1); err != nil {
		t.Fatal(err)
	}

	// World 2 occupies recycled slot 1 with its own unique identity
	w2ID := domain.NewID()
	if err := st.UpsertInstance(InstanceRecord{
		Number: 1, WorldID: w2ID, Name: "World 2", Slug: "world-2", Tier: "medium", State: "stopped",
	}); err != nil {
		t.Fatal(err)
	}

	// World 2 must have an empty crashes tab!
	w2Last, err := st.LastIncident(ctx, w2ID)
	if err != nil || w2Last != nil {
		t.Fatalf("world 2 must not inherit dead world's crash; got %v, err=%v", w2Last, err)
	}
	w2List, err := st.ListIncidents(ctx, w2ID, 10, false)
	if err != nil || len(w2List) != 0 {
		t.Fatalf("world 2 must have empty crashes list; got %d rows", len(w2List))
	}
	w2Unack, err := st.LastUnacknowledgedIncident(ctx, w2ID)
	if err != nil || w2Unack != nil {
		t.Fatalf("world 2 must have no unacknowledged crashes; got %v", w2Unack)
	}

	// World 1's history still survives in the store
	w1History, err := st.ListIncidents(ctx, w1ID, 10, false)
	if err != nil || len(w1History) != 2 {
		t.Fatalf("world 1 history should survive in store, got %d rows", len(w1History))
	}

	// Querying with empty worldID returns nil without error
	emptyLast, err := st.LastIncident(ctx, "")
	if err != nil || emptyLast != nil {
		t.Errorf("empty worldID query should return nil, got %v, %v", emptyLast, err)
	}
	emptyList, err := st.ListIncidents(ctx, "", 10, false)
	if err != nil || len(emptyList) != 0 {
		t.Errorf("empty worldID list query should return empty slice, got %v, %v", emptyList, err)
	}
}
