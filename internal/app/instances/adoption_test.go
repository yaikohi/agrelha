package instances

import (
	"context"
	"fmt"
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

func TestAdoptLegacyValheim_NoWorkload(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test-no-workload.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	repo := store.NewValheimInstanceRepo(st)
	ctx := context.Background()
	legacyRef := ports.ServerRef{Name: "valheim", Scope: "valheim"}

	// 1. rt is nil (local testing mode)
	adopted, err := AdoptLegacyValheim(ctx, repo, nil, legacyRef, "192.168.20.224", "Valheim Legacy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adopted != nil {
		t.Fatalf("expected nil when runtime is nil, got %v", adopted)
	}
	insts, _ := repo.List()
	if len(insts) != 0 {
		t.Fatalf("expected 0 instances in repo, got %d", len(insts))
	}

	// 2. rt returns LifecycleUnknown (workload not deployed in cluster)
	rtUnknown := &mockRuntimeForAdoption{
		status: ports.Status{Lifecycle: ports.LifecycleUnknown},
	}
	adopted, err = AdoptLegacyValheim(ctx, repo, rtUnknown, legacyRef, "192.168.20.224", "Valheim Legacy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adopted != nil {
		t.Fatalf("expected nil when workload is unknown, got %v", adopted)
	}
	insts, _ = repo.List()
	if len(insts) != 0 {
		t.Fatalf("expected 0 instances in repo, got %d", len(insts))
	}
}

type mockRepoForAdoption struct {
	getInst   *domain.Instance
	getErr    error
	upsertErr error
}

func (m *mockRepoForAdoption) List() ([]domain.Instance, error) { return nil, nil }
func (m *mockRepoForAdoption) Get(num int) (*domain.Instance, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.getInst, nil
}
func (m *mockRepoForAdoption) Upsert(inst domain.Instance) error {
	return m.upsertErr
}
func (m *mockRepoForAdoption) UpdateState(num int, state domain.InstanceState) error {
	return nil
}
func (m *mockRepoForAdoption) Delete(num int) error { return nil }

type mockRuntimeErr struct {
	mockRuntimeForAdoption
	statusErr error
}

func (m *mockRuntimeErr) Status(ctx context.Context, ref ports.ServerRef) (ports.Status, error) {
	return ports.Status{}, m.statusErr
}

func TestAdoptLegacyValheimEdgeCases(t *testing.T) {
	ctx := context.Background()
	ref := ports.ServerRef{Name: "valheim"}
	rt := &mockRuntimeForAdoption{status: ports.Status{Lifecycle: ports.LifecycleRunning}}

	// 1. repo.Get error
	repoGetErr := &mockRepoForAdoption{getErr: fmt.Errorf("db fail")}
	if _, err := AdoptLegacyValheim(ctx, repoGetErr, rt, ref, "1.2.3.4", "Name"); err == nil {
		t.Errorf("expected error when repo.Get fails")
	}

	// 2. rt.Status error
	rtErr := &mockRuntimeErr{statusErr: fmt.Errorf("status fail")}
	repoOk := &mockRepoForAdoption{}
	if inst, err := AdoptLegacyValheim(ctx, repoOk, rtErr, ref, "1.2.3.4", "Name"); err != nil || inst != nil {
		t.Errorf("expected nil when status errors: %v, %v", inst, err)
	}

	// 3. serverName == "" and slug fallback
	repoUpsertErr := &mockRepoForAdoption{upsertErr: fmt.Errorf("upsert fail")}
	if _, err := AdoptLegacyValheim(ctx, repoUpsertErr, rt, ref, "1.2.3.4", ""); err == nil {
		t.Errorf("expected error when repo.Upsert fails")
	}

	// 4. slug == "default" or empty
	repoDefault := &mockRepoForAdoption{}
	inst, err := AdoptLegacyValheim(ctx, repoDefault, rt, ref, "1.2.3.4", "default")
	if err != nil || inst == nil || inst.Slug != "server" {
		t.Errorf("expected slug 'server' for default name, got %+v, err: %v", inst, err)
	}

	// 5. empty ref
	if inst, err := AdoptLegacyValheim(ctx, repoDefault, rt, ports.ServerRef{}, "1.2.3.4", "name"); err != nil || inst != nil {
		t.Errorf("expected nil for empty ref")
	}
}
