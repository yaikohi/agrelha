package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"agrelha/internal/domain"
)

func ackStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "ack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestDismissingAFailureStopsItDemandingAttention(t *testing.T) {
	s := ackStore(t)
	ctx := context.Background()

	id, err := s.RecordIncident(ctx, domain.Incident{
		GameID: domain.GameValheim, Number: 2, At: time.Now().Add(-time.Hour),
		ExitCode: 137, OOMKilled: true, Reason: "OOMKilled",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got, err := s.LastUnacknowledgedIncident(ctx, domain.GameValheim, 2); err != nil || got == nil {
		t.Fatalf("a fresh failure must surface on the Overview; got %v err %v", got, err)
	}

	if err := s.AcknowledgeIncident(ctx, id); err != nil {
		t.Fatal(err)
	}

	got, err := s.LastUnacknowledgedIncident(ctx, domain.GameValheim, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf(`a dismissed failure still surfaces on the Overview.

The panel reads the newest unacknowledged incident. If dismissing does not persist, the × un-dismisses
itself on the next page load, which is worse than having no button at all.`)
	}
}

func TestADismissedFailureStaysInTheHistory(t *testing.T) {
	s := ackStore(t)
	ctx := context.Background()

	id, _ := s.RecordIncident(ctx, domain.Incident{
		GameID: domain.GameValheim, Number: 2, At: time.Now().Add(-time.Hour), ExitCode: 1,
	})
	if err := s.AcknowledgeIncident(ctx, id); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListIncidents(ctx, domain.GameValheim, 2, 20, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("dismissing deleted the failure from history; got %d rows, want 1", len(list))
	}
	if !list[0].Acknowledged {
		t.Error("the Crashes tab cannot tell a dismissed failure from a live one")
	}
}

func TestDismissingIsIdempotent(t *testing.T) {
	s := ackStore(t)
	ctx := context.Background()
	id, _ := s.RecordIncident(ctx, domain.Incident{GameID: domain.GameValheim, Number: 1, At: time.Now(), ExitCode: 1})
	if err := s.AcknowledgeIncident(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.AcknowledgeIncident(ctx, id); err != nil {
		t.Errorf("dismissing twice errored: %v", err)
	}
}

func TestANewFailureAfterDismissalSurfacesAgain(t *testing.T) {
	s := ackStore(t)
	ctx := context.Background()
	old, _ := s.RecordIncident(ctx, domain.Incident{GameID: domain.GameValheim, Number: 1, At: time.Now().Add(-2 * time.Hour), ExitCode: 1})
	_ = s.AcknowledgeIncident(ctx, old)

	if _, err := s.RecordIncident(ctx, domain.Incident{GameID: domain.GameValheim, Number: 1, At: time.Now(), ExitCode: 137}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LastUnacknowledgedIncident(ctx, domain.GameValheim, 1)
	if err != nil || got == nil {
		t.Fatalf("a crash that happened after a dismissal must surface; got %v err %v", got, err)
	}
	if got.ExitCode != 137 {
		t.Errorf("surfaced the wrong failure: exit %d", got.ExitCode)
	}
}

func TestTheCrashLedgerSortsBothWays(t *testing.T) {
	s := ackStore(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

	for i, at := range []time.Time{base, base.Add(2 * time.Hour), base.Add(4 * time.Hour)} {
		if _, err := s.RecordIncident(ctx, domain.Incident{
			GameID: domain.GameValheim, Number: 9, At: at, ExitCode: int32(i + 1),
		}); err != nil {
			t.Fatal(err)
		}
	}

	newest, err := s.ListIncidents(ctx, domain.GameValheim, 9, 20, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(newest) != 3 || !newest[0].At.Equal(base.Add(4*time.Hour)) {
		t.Fatalf("default order must be newest first; got %d rows starting %v", len(newest), newest[0].At)
	}

	oldest, err := s.ListIncidents(ctx, domain.GameValheim, 9, 20, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldest) != 3 || !oldest[0].At.Equal(base) {
		t.Fatalf("oldest-first must start at the earliest failure; got %v", oldest[0].At)
	}
}

func TestSortingIsDoneInTheQueryNotAfterTheLimit(t *testing.T) {
	s := ackStore(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for i := range 5 {
		if _, err := s.RecordIncident(ctx, domain.Incident{
			GameID: domain.GameValheim, Number: 8, At: base.Add(time.Duration(i) * time.Hour), ExitCode: int32(i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	oldest, err := s.ListIncidents(ctx, domain.GameValheim, 8, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldest) != 2 || !oldest[0].At.Equal(base) {
		t.Errorf(`oldest-first with a limit returned %v, not the two earliest failures.

Reversing a newest-first page in Go would give the oldest *of that page*, which is the newest rows -
silently the wrong answer once a world has more failures than the limit.`, oldest)
	}
}

func TestTheWatcherStillSeesDismissedFailuresSoItDoesNotRecordThemTwice(t *testing.T) {
	s := ackStore(t)
	ctx := context.Background()

	id, err := s.RecordIncident(ctx, domain.Incident{
		GameID: domain.GameValheim, Number: 4, At: time.Now().Add(-time.Hour), ExitCode: 137, RestartCount: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AcknowledgeIncident(ctx, id); err != nil {
		t.Fatal(err)
	}

	last, err := s.LastIncident(ctx, domain.GameValheim, 4)
	if err != nil {
		t.Fatal(err)
	}
	if last == nil {
		t.Error(`LastIncident hid a dismissed failure from the health watcher.

The watcher compares the runtime's current failure against the last one it recorded to decide whether
anything new happened. If dismissing hides that row, every tick looks like a brand-new crash and the
ledger fills with duplicates of the failure the operator just dismissed.`)
	}
}
