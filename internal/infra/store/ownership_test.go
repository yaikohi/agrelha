package store

import (
	"context"
	"slices"
	"testing"

	"agrelha/internal/domain"
)

func TestInstancesCreatedBy(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mustUpsert := func(rec InstanceRecord, valheim bool) {
		t.Helper()
		var err error
		if valheim {
			err = s.UpsertValheimInstance(rec)
		} else {
			err = s.UpsertInstance(rec)
		}
		if err != nil {
			t.Fatalf("upsert #%d: %v", rec.Number, err)
		}
	}

	mustUpsert(InstanceRecord{Number: 1, Name: "mine", Slug: "mine", Tier: "medium", State: "stopped", CreatedBy: "user-a"}, false)
	mustUpsert(InstanceRecord{Number: 2, Name: "theirs", Slug: "theirs", Tier: "medium", State: "stopped", CreatedBy: "user-b"}, false)
	mustUpsert(InstanceRecord{Number: 1, Name: "vmine", Slug: "vmine", Tier: "medium", State: "stopped", CreatedBy: "user-a"}, true)

	got, err := s.InstancesCreatedBy(ctx, "user-a")
	if err != nil {
		t.Fatalf("InstancesCreatedBy: %v", err)
	}

	if !slices.Contains(got, domain.NewInstanceID(domain.GameMinecraft, 1)) {
		t.Errorf("missing minecraft #1, got %v", got)
	}
	if !slices.Contains(got, domain.NewInstanceID(domain.GameValheim, 1)) {
		t.Errorf("ownership must span games, not just the first table scanned; got %v", got)
	}
	if slices.Contains(got, domain.NewInstanceID(domain.GameMinecraft, 2)) {
		t.Errorf("returned a world created by somebody else, got %v", got)
	}

	none, err := s.InstancesCreatedBy(ctx, "nobody")
	if err != nil {
		t.Fatalf("InstancesCreatedBy(nobody): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("expected no worlds for an unknown subject, got %v", none)
	}
}
