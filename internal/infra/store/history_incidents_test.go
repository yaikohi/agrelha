package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"agrelha/internal/domain"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestHistoryIncludesCrashesWithTheirLogs(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	if _, err := st.RecordIncident(ctx, domain.Incident{
		GameID: domain.GameValheim, Number: 2, At: time.Now(),
		ExitCode: 137, OOMKilled: true, Reason: "OOMKilled",
		LogTail: "line one\nline two",
	}); err != nil {
		t.Fatal(err)
	}
	_ = st.RecordAudit("ykhi", "mod-install", "something")

	entries, err := st.ListHistory(50)
	if err != nil {
		t.Fatal(err)
	}

	var crash *domain.HistoryEntry
	for i := range entries {
		if entries[i].Incident != nil {
			crash = &entries[i]
		}
	}
	if crash == nil {
		t.Fatal("a recorded crash must appear in history; it was invisible before")
	}
	if crash.Kind != "crash" || crash.Source != "incident" {
		t.Errorf("unexpected shape: %+v", crash)
	}
	if crash.Incident.LogTail != "line one\nline two" {
		t.Error("the stored log tail must come through so it can be revisited")
	}
	if crash.Detail == "" {
		t.Error("the row needs a readable summary")
	}
	if !crash.Incident.OOMKilled || crash.Incident.ExitCode != 137 {
		t.Errorf("incident fields lost: %+v", crash.Incident)
	}
}

func TestHistoryOrdersEverythingTogetherNewestFirst(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	old := time.Now().Add(-2 * time.Hour)
	if _, err := st.RecordIncident(ctx, domain.Incident{
		GameID: domain.GameValheim, Number: 1, At: old, ExitCode: 1, LogTail: "x",
	}); err != nil {
		t.Fatal(err)
	}
	_ = st.RecordAudit("ykhi", "restart", "later")

	entries, err := st.ListHistory(50)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("want both sources, got %d", len(entries))
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].At.After(entries[i-1].At) {
			t.Errorf("entries out of order at %d: %v then %v", i, entries[i-1].At, entries[i].At)
		}
	}
}

func TestPruningDropsOldEventsAndKeepsCrashes(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	if _, err := st.RecordIncident(ctx, domain.Incident{
		GameID: domain.GameValheim, Number: 1,
		At: time.Now().Add(-365 * 24 * time.Hour), ExitCode: 1, LogTail: "ancient",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO events (at, kind, detail) VALUES (?,?,?)`,
		time.Now().Add(-365*24*time.Hour), "join", "old"); err != nil {
		t.Fatal(err)
	}
	_ = st.RecordEvent("join", "recent")

	n, err := st.PruneEvents(time.Now().Add(-90 * 24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("want 1 old event pruned, got %d", n)
	}

	entries, err := st.ListHistory(50)
	if err != nil {
		t.Fatal(err)
	}
	var sawCrash, sawOldJoin bool
	for _, e := range entries {
		if e.Incident != nil {
			sawCrash = true
		}
		if e.Detail == "old" {
			sawOldJoin = true
		}
	}
	if !sawCrash {
		t.Error("a year-old crash is exactly what you want to still have")
	}
	if sawOldJoin {
		t.Error("a year-old join is noise and should have been pruned")
	}
}
