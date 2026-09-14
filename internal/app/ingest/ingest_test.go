package ingest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
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
	cleared      bool
	seen         []string
	online       map[string]bool
	events       []string
	errClear     error
	errUpsert    error
	errSetOnline error
	errRecord    error
}

func (m *mockStore) ClearPresence() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleared = true
	return m.errClear
}

func (m *mockStore) UpsertSeen(id, character string, joined bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seen = append(m.seen, id+":"+character)
	return m.errUpsert
}

func (m *mockStore) SetOnline(id string, online bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.online == nil {
		m.online = make(map[string]bool)
	}
	m.online[id] = online
	return m.errSetOnline
}

func (m *mockStore) RecordEvent(kind, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, kind+":"+id)
	return m.errRecord
}

func TestConsume(t *testing.T) {
	// 1. StreamLogs error
	streamErr := errors.New("stream error")
	sFailing := &mockStreamer{
		streamFn: func(ctx context.Context, tail int64) (io.ReadCloser, error) {
			return nil, streamErr
		},
	}
	st := &mockStore{}
	if err := consume(context.Background(), sFailing, st); !errors.Is(err, streamErr) {
		t.Fatalf("expected stream error, got %v", err)
	}

	// 2. Successful parsing of join, char with lastSteam, char without lastSteam, disconnect, and noise
	logLines := strings.Join([]string{
		"Some random server startup line",
		"Got character ZDOID from Ignored : 123", // lastSteam is empty here
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
	if err := consume(context.Background(), sSuccess, stSuccess); err != nil {
		t.Fatalf("consume failed: %v", err)
	}

	if len(stSuccess.seen) != 2 {
		t.Fatalf("expected 2 seen calls, got %d (%v)", len(stSuccess.seen), stSuccess.seen)
	}
	if stSuccess.seen[0] != "76561198000000001:" {
		t.Errorf("expected initial steam seen, got %s", stSuccess.seen[0])
	}
	if stSuccess.seen[1] != "76561198000000001:Ragnar" {
		t.Errorf("expected character seen, got %s", stSuccess.seen[1])
	}

	if stSuccess.online["76561198000000001"] != false {
		// It connected and then closed socket, so final state is false
		t.Errorf("expected online=false after dc, got %v", stSuccess.online["76561198000000001"])
	}

	if len(stSuccess.events) != 2 || stSuccess.events[0] != "join:76561198000000001" || stSuccess.events[1] != "leave:76561198000000001" {
		t.Errorf("unexpected events: %v", stSuccess.events)
	}

	// 3. Scanner error (line too long > 1MB)
	hugeLine := bytes.Repeat([]byte("a"), 1024*1024+10)
	sHuge := &mockStreamer{
		streamFn: func(ctx context.Context, tail int64) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(hugeLine)), nil
		},
	}
	stHuge := &mockStore{}
	if err := consume(context.Background(), sHuge, stHuge); err == nil {
		t.Fatalf("expected scanner error on huge line")
	}
}

func TestRun(t *testing.T) {
	oldDelay := retryDelay
	retryDelay = time.Millisecond
	defer func() { retryDelay = oldDelay }()

	// 1. Run with already-canceled context
	ctxCanceled, cancel := context.WithCancel(context.Background())
	cancel()

	st := &mockStore{}
	s := &mockStreamer{}
	Run(ctxCanceled, s, st)
	if !st.cleared {
		t.Errorf("expected ClearPresence to be called")
	}

	// 2. Run with error retry loop that exits on context cancel
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
	Run(ctxTimeout, sRetry, stRetry)

	if errCount == 0 {
		t.Errorf("expected at least one stream call in retry loop")
	}
}
