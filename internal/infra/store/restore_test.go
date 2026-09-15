package store

import (
	"testing"

	"agrelha/internal/domain"
)

func TestRestorePointRoundTrip(t *testing.T) {
	s := newTestStore(t)

	prev := []string{"denikson/BepInExPack_Valheim/5.4.2202", "Smoothbrain/Mining/1.3.4"}
	applied := []string{"denikson/BepInExPack_Valheim/5.4.2202", "Smoothbrain/Mining/1.3.9"}
	if err := s.SaveRestorePoint(domain.GameValheim, 2, prev, applied); err != nil {
		t.Fatalf("SaveRestorePoint: %v", err)
	}

	rp, err := s.RestorePoint(domain.GameValheim, 2)
	if err != nil || rp == nil {
		t.Fatalf("RestorePoint: %v, %v", rp, err)
	}
	if !rp.Matches(applied) {
		t.Error("the point must match the list its update wrote")
	}
	if rp.Matches(prev) {
		t.Error("the point must not match the list from before the update")
	}
	if len(rp.Previous) != 2 || rp.Previous[1] != "Smoothbrain/Mining/1.3.4" {
		t.Errorf("Previous = %v, want the pre-update pins", rp.Previous)
	}
	if rp.At.IsZero() {
		t.Error("want a timestamp, so the UI can say how old the way back is")
	}
}

// One point per Instance: a second update replaces the first, so undo is always
// one step and never walks back further than the operator expects.
func TestSaveRestorePointReplacesTheOldOne(t *testing.T) {
	s := newTestStore(t)

	if err := s.SaveRestorePoint(domain.GameValheim, 1, []string{"A/B/1.0.0"}, []string{"A/B/2.0.0"}); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := s.SaveRestorePoint(domain.GameValheim, 1, []string{"A/B/2.0.0"}, []string{"A/B/3.0.0"}); err != nil {
		t.Fatalf("second save: %v", err)
	}

	rp, err := s.RestorePoint(domain.GameValheim, 1)
	if err != nil || rp == nil {
		t.Fatalf("RestorePoint: %v, %v", rp, err)
	}
	if len(rp.Previous) != 1 || rp.Previous[0] != "A/B/2.0.0" {
		t.Errorf("Previous = %v, want only the most recent step back", rp.Previous)
	}
}

func TestRestorePointsAreScopedPerInstanceAndGame(t *testing.T) {
	s := newTestStore(t)

	if err := s.SaveRestorePoint(domain.GameValheim, 1, []string{"A/B/1.0.0"}, []string{"A/B/2.0.0"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	other, err := s.RestorePoint(domain.GameValheim, 2)
	if err != nil {
		t.Fatalf("RestorePoint: %v", err)
	}
	if other != nil {
		t.Error("a restore point on one world must not appear on another")
	}
	if mc, _ := s.RestorePoint(domain.GameMinecraft, 1); mc != nil {
		t.Error("a restore point must not cross games")
	}

	if err := s.ClearRestorePoint(domain.GameValheim, 1); err != nil {
		t.Fatalf("ClearRestorePoint: %v", err)
	}
	if rp, _ := s.RestorePoint(domain.GameValheim, 1); rp != nil {
		t.Error("want the point gone after clearing")
	}
}

func TestRestorePoint_EmptyLists(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveRestorePoint(domain.GameMinecraft, 5, nil, nil); err != nil {
		t.Fatalf("SaveRestorePoint empty lists failed: %v", err)
	}
	rp, err := s.RestorePoint(domain.GameMinecraft, 5)
	if err != nil {
		t.Fatalf("RestorePoint failed: %v", err)
	}
	if rp == nil || rp.Previous != nil || rp.Applied != nil {
		t.Errorf("expected empty slices converted to nil, got %+v", rp)
	}
}

