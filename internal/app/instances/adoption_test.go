package instances

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
)

type mockRuntimeForAdoption struct {
	status ports.Status
}

func (m *mockRuntimeForAdoption) Start(ctx context.Context, ref ports.ServerRef) error   { return nil }
func (m *mockRuntimeForAdoption) Stop(ctx context.Context, ref ports.ServerRef) error    { return nil }
func (m *mockRuntimeForAdoption) Restart(ctx context.Context, ref ports.ServerRef) error { return nil }
func (m *mockRuntimeForAdoption) Status(ctx context.Context, ref ports.ServerRef) (ports.Status, error) {
	return m.status, nil
}
func (m *mockRuntimeForAdoption) Metrics(ctx context.Context, ref ports.ServerRef) (ports.Metrics, error) {
	return ports.Metrics{}, nil
}
func (m *mockRuntimeForAdoption) Logs(ctx context.Context, ref ports.ServerRef, opts ports.LogOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockRuntimeForAdoption) WatchAvailability(ctx context.Context, ref ports.ServerRef, timeout time.Duration) error {
	return nil
}

func TestAdoptLegacyValheim(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test-adoption.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	repo := store.NewValheimInstanceRepo(st)
	ctx := context.Background()
	legacyRef := ports.ServerRef{Name: "valheim", Scope: "valheim"}

	// 1. Adopt when runtime is running
	rt := &mockRuntimeForAdoption{
		status: ports.Status{Lifecycle: ports.LifecycleRunning, Available: true},
	}

	adopted, err := AdoptLegacyValheim(ctx, repo, rt, legacyRef, "192.168.20.224", "Valheim Legacy")
	if err != nil {
		t.Fatalf("AdoptLegacyValheim failed: %v", err)
	}
	if adopted == nil {
		t.Fatalf("expected adopted instance, got nil")
	}
	if adopted.Number != 1 {
		t.Errorf("expected slot 1, got %d", adopted.Number)
	}
	if adopted.GameID != domain.GameValheim {
		t.Errorf("expected GameValheim, got %s", adopted.GameID)
	}
	if adopted.State != domain.StateRunning {
		t.Errorf("expected running state from runtime probe, got %s", adopted.State)
	}
	if adopted.LBIP != "192.168.20.224" {
		t.Errorf("expected LBIP 192.168.20.224, got %s", adopted.LBIP)
	}
	if adopted.Tier != domain.TierMedium {
		t.Errorf("expected medium tier, got %s", adopted.Tier)
	}

	// 2. Second invocation should be a no-op and return existing slot 1
	second, err := AdoptLegacyValheim(ctx, repo, rt, legacyRef, "192.168.20.224", "Different Name")
	if err != nil {
		t.Fatalf("second AdoptLegacyValheim failed: %v", err)
	}
	if second.Name != "Valheim Legacy" {
		t.Errorf("expected original name preserved, got %s", second.Name)
	}
}
