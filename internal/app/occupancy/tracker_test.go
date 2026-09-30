package occupancy

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type streamer struct {
	mu         sync.Mutex
	bodies     []string
	err        error
	opened     int
	blockCh    chan struct{}
	blockAfter int
}

func (s *streamer) StreamLogs(ctx context.Context, tail int64) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	if s.opened >= len(s.bodies) {
		if s.blockCh != nil && len(s.bodies) > 0 {
			return blocking{Reader: strings.NewReader(s.bodies[len(s.bodies)-1]), done: s.blockCh}, nil
		}
		return nil, errors.New("no more streams")
	}
	idx := s.opened
	body := s.bodies[idx]
	s.opened++
	if s.blockCh != nil && idx >= s.blockAfter {
		return blocking{Reader: strings.NewReader(body), done: s.blockCh}, nil
	}
	return io.NopCloser(strings.NewReader(body)), nil
}

type blocking struct {
	io.Reader
	done chan struct{}
	eof  bool
}

func (b blocking) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if err == io.EOF {
		<-b.done
		return n, io.EOF
	}
	return n, err
}

func (b blocking) Close() error { return nil }

const joined = "10/01 12:00:00: Got connection SteamID 76561190000000001\n"
const joined2 = "10/01 12:00:05: Got connection SteamID 76561190000000002\n"
const left = "10/01 12:01:00: Closing socket 76561190000000001\n"

func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not reached in time")
}

func TestCountsWhoIsConnected(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	s := &streamer{bodies: []string{joined + joined2}, blockCh: done}

	tr := New(WithSettle(0))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr.Watch(ctx, 1, s)

	waitFor(t, func() bool { n, ok := tr.Players(1); return ok && n == 2 })
}

func TestADisconnectRemovesThePlayer(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	s := &streamer{bodies: []string{joined + joined2 + left}, blockCh: done}

	tr := New(WithSettle(0))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr.Watch(ctx, 1, s)

	waitFor(t, func() bool { n, ok := tr.Players(1); return ok && n == 1 })
}

func TestAWorldNobodyIsWatchingIsUnknown(t *testing.T) {
	tr := New()
	if n, ok := tr.Players(7); ok || n != 0 {
		t.Errorf("an unwatched world is unknown, got %d known=%v", n, ok)
	}
	var nilTracker *Tracker
	if _, ok := nilTracker.Players(1); ok {
		t.Error("a nil tracker knows nothing")
	}
}

func TestAFreshStreamIsUnknownUntilItHasSettled(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	s := &streamer{bodies: []string{joined}, blockCh: done}

	now := time.Now()
	tr := New(WithSettle(time.Minute), WithClock(func() time.Time { return now }))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr.Watch(ctx, 1, s)

	time.Sleep(50 * time.Millisecond)
	if _, ok := tr.Players(1); ok {
		t.Error("a stream still replaying the log must not be reported as a known count")
	}

	now = now.Add(2 * time.Minute)
	waitFor(t, func() bool { n, ok := tr.Players(1); return ok && n == 1 })
}

func TestALostStreamGoesBackToUnknown(t *testing.T) {
	s := &streamer{bodies: []string{joined}}

	tr := New(WithSettle(0), WithRetry(time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr.Watch(ctx, 1, s)

	waitFor(t, func() bool { _, ok := tr.Players(1); return !ok })
}

func TestAReconnectStartsFromTheLogAgainRatherThanAccumulating(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	s := &streamer{
		bodies:     []string{joined + joined2, joined},
		blockCh:    done,
		blockAfter: 1,
	}

	tr := New(WithSettle(0), WithRetry(time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr.Watch(ctx, 1, s)

	waitFor(t, func() bool {
		s.mu.Lock()
		opened := s.opened
		s.mu.Unlock()
		n, ok := tr.Players(1)
		return opened >= 2 && ok && n == 1
	})
}

func TestWatchIgnoresANilStreamer(t *testing.T) {
	tr := New()
	tr.Watch(context.Background(), 1, nil)
	if _, ok := tr.Players(1); ok {
		t.Error("nothing to watch means nothing known")
	}
}
