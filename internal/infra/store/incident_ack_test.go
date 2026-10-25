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

	if got, err := s.LastIncident(ctx, domain.GameValheim, 2); err != nil || got == nil {
		t.Fatalf("a fresh failure must surface on the Overview; got %v err %v", got, err)
	}

	if err := s.AcknowledgeIncident(ctx, id); err != nil {
		t.Fatal(err)
	}

	got, err := s.LastIncident(ctx, domain.GameValheim, 2)
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

	list, err := s.ListIncidents(ctx, domain.GameValheim, 2, 20)
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
	got, err := s.LastIncident(ctx, domain.GameValheim, 1)
	if err != nil || got == nil {
		t.Fatalf("a crash that happened after a dismissal must surface; got %v err %v", got, err)
	}
	if got.ExitCode != 137 {
		t.Errorf("surfaced the wrong failure: exit %d", got.ExitCode)
	}
}
