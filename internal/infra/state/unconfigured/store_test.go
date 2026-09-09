package unconfigured

import (
	"context"
	"errors"
	"testing"

	"agrelha/internal/ports"
)

func TestGetReturnsEmptyDocument(t *testing.T) {
	doc, err := New().Get(context.Background(), "manifests/mods.yaml")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(doc.Data) != 0 {
		t.Fatalf("expected empty data, got %v", doc.Data)
	}
	if doc.Data == nil || doc.Annotations == nil {
		t.Fatal("maps must be non-nil so callers can range without a guard")
	}
}

func TestWritesReportUnconfigured(t *testing.T) {
	s := New()
	ctx := context.Background()

	if err := s.Put(ctx, "p", ports.Document{}, "m"); !errors.Is(err, ErrUnconfigured) {
		t.Errorf("Put: got %v", err)
	}
	if _, err := s.Patch(ctx, "p", "m", func(*ports.Document) (bool, error) { return true, nil }); !errors.Is(err, ErrUnconfigured) {
		t.Errorf("Patch: got %v", err)
	}
	if err := s.Delete(ctx, "p", "m"); !errors.Is(err, ErrUnconfigured) {
		t.Errorf("Delete: got %v", err)
	}
	if err := s.PutTree(ctx, "d", nil, "m"); !errors.Is(err, ErrUnconfigured) {
		t.Errorf("PutTree: got %v", err)
	}
}
