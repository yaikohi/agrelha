package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"agrelha/internal/domain"
)

type mockStreamer struct {
	streamFn func(ctx context.Context, tail int64) (io.ReadCloser, error)
}

func (m *mockStreamer) StreamLogs(ctx context.Context, tail int64) (io.ReadCloser, error) {
	if m.streamFn != nil {
		return m.streamFn(ctx, tail)
	}
	return io.NopCloser(strings.NewReader("")), nil
}

type mockStore struct {
	mu           sync.Mutex
	cleared      []int
	seen         []string
	online       map[string]bool
	events       []string
	errClear     error
	errUpsert    error
	errSetOnline error
	errRecord    error
}

func (m *mockStore) ClearPresence(instance int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleared = append(m.cleared, instance)
	return m.errClear
}

func (m *mockStore) UpsertSeen(instance int, id, character string, joined bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seen = append(m.seen, fmt.Sprintf("%d:%s:%s", instance, id, character))
	return m.errUpsert
}

func (m *mockStore) SetOnline(instance int, id string, online bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.online == nil {
		m.online = make(map[string]bool)
	}
	m.online[fmt.Sprintf("%d:%s", instance, id)] = online
	return m.errSetOnline
}

func (m *mockStore) RecordEvent(kind, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, kind+":"+id)
	return m.errRecord
}

func TestConsume(t *testing.T) {
	streamErr := errors.New("stream error")
	sFailing := &mockStreamer{
		streamFn: func(ctx context.Context, tail int64) (io.ReadCloser, error) {
			return nil, streamErr
		},
	}
	st := &mockStore{}
	if err := consume(context.Background(), 1, sFailing, st); !errors.Is(err, streamErr) {
		t.Fatalf("expected stream error, got %v", err)
	}

	logLines := strings.Join([]string{
		"Some random server startup line",
		"Got character ZDOID from Ignored : 123",
		"Got connection SteamID 76561198000000001",
		"Got character ZDOID from Ragnar : 456",
		"Closing socket 76561198000000001",
		"Another uninteresting line",
	}, "\n")

	sSuccess := &mockStreamer{
		streamFn: func(ctx context.Context, tail int64) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(logLines)), nil
		},
	}

	stSuccess := &mockStore{}
	if err := consume(context.Background(), 2, sSuccess, stSuccess); err != nil {
		t.Fatalf("consume failed: %v", err)
	}

	if len(stSuccess.seen) != 2 {
		t.Fatalf("expected 2 seen calls, got %d (%v)", len(stSuccess.seen), stSuccess.seen)
	}
	if stSuccess.seen[0] != "2:76561198000000001:" {
		t.Errorf("expected initial steam seen with instance 2, got %s", stSuccess.seen[0])
	}
	if stSuccess.seen[1] != "2:76561198000000001:Ragnar" {
		t.Errorf("expected character seen with instance 2, got %s", stSuccess.seen[1])
	}

	if stSuccess.online["2:76561198000000001"] != false {
		t.Errorf("expected online=false after dc on instance 2, got %v", stSuccess.online["2:76561198000000001"])
	}

	if len(stSuccess.events) != 2 || stSuccess.events[0] != "join:76561198000000001" || stSuccess.events[1] != "leave:76561198000000001" {
		t.Errorf("unexpected events: %v", stSuccess.events)
	}

	hugeLine := bytes.Repeat([]byte("a"), 1024*1024+10)
	sHuge := &mockStreamer{
		streamFn: func(ctx context.Context, tail int64) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(hugeLine)), nil
		},
	}
	stHuge := &mockStore{}
	if err := consume(context.Background(), 1, sHuge, stHuge); err == nil {
		t.Fatalf("expected scanner error on huge line")
	}
}

func TestRun(t *testing.T) {
	ctxCanceled, cancel := context.WithCancel(context.Background())
	cancel()

	st := &mockStore{}
	s := &mockStreamer{}
	Run(ctxCanceled, 1, s, st)
	if len(st.cleared) == 0 {
		t.Errorf("expected ClearPresence to be called")
	}

	ctxTimeout, cancelTimeout := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancelTimeout()

	errCount := 0
	sRetry := &mockStreamer{
		streamFn: func(ctx context.Context, tail int64) (io.ReadCloser, error) {
			errCount++
			return nil, errors.New("temporary stream error")
		},
	}
	stRetry := &mockStore{}
	Run(ctxTimeout, 1, sRetry, stRetry, time.Millisecond, 2*time.Millisecond)

	if errCount == 0 {
		t.Errorf("expected at least one stream call in retry loop")
	}
}

type fakeInstanceManager struct {
	mu        sync.Mutex
	instances []domain.Instance
}

func (f *fakeInstanceManager) ListInstances(ctx context.Context) ([]domain.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.Instance, len(f.instances))
	copy(out, f.instances)
	return out, nil
}

func (f *fakeInstanceManager) InstanceLogs(ctx context.Context, instanceNumber int, tail int64) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func TestSupervisorReconciliationAndWorkerLifecycle(t *testing.T) {

	mgr := &fakeInstanceManager{
		instances: []domain.Instance{
			{Number: 1, State: domain.StateRunning},
			{Number: 2, State: domain.StateStopped},
		},
	}
	st := &mockStore{}
	sup := NewSupervisor(mgr, mgr, st, 10*time.Millisecond)

	sup.Reconcile(context.Background())
	active := sup.ActiveWorkers()
	if len(active) != 1 || active[0] != 1 {
		t.Fatalf("supervisor must spawn worker only for running instances, got active workers %v; stopped instances are unnecessarily consuming log streams", active)
	}

	mgr.mu.Lock()
	mgr.instances = []domain.Instance{
		{Number: 1, State: domain.StateRunning},
		{Number: 2, State: domain.StateRunning},
	}
	mgr.mu.Unlock()

	sup.Reconcile(context.Background())
	active = sup.ActiveWorkers()
	if len(active) != 2 {
		t.Fatalf("supervisor must spawn worker for newly running instance 2, got %d workers (%v); world 2 presence is not being tracked", len(active), active)
	}

	mgr.mu.Lock()
	mgr.instances = []domain.Instance{
		{Number: 1, State: domain.StateStopped},
		{Number: 2, State: domain.StateRunning},
	}
	mgr.mu.Unlock()

	sup.Reconcile(context.Background())
	active = sup.ActiveWorkers()
	if len(active) != 1 || active[0] != 2 {
		t.Fatalf("supervisor must terminate worker for stopped instance 1, got %v; dead instances are leaking goroutines", active)
	}

	sup.stopAll()
	if len(sup.ActiveWorkers()) != 0 {
		t.Fatalf("stopAll must cancel all workers, got %d active", len(sup.ActiveWorkers()))
	}
}
