package occupancy

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"agrelha/internal/domain"
)

type Streamer interface {
	StreamLogs(ctx context.Context, tail int64) (io.ReadCloser, error)
}

type Option func(*Tracker)

func WithSettle(d time.Duration) Option    { return func(t *Tracker) { t.settle = d } }
func WithRetry(d time.Duration) Option     { return func(t *Tracker) { t.retry = d } }
func WithTail(n int64) Option              { return func(t *Tracker) { t.tail = n } }
func WithClock(fn func() time.Time) Option { return func(t *Tracker) { t.now = fn } }

type world struct {
	online      map[string]struct{}
	connectedAt time.Time
	live        bool
}

type Tracker struct {
	mu    sync.Mutex
	state map[int]*world

	settle time.Duration
	retry  time.Duration
	tail   int64
	now    func() time.Time
}

func New(opts ...Option) *Tracker {
	t := &Tracker{
		state:  map[int]*world{},
		settle: 10 * time.Second,
		retry:  5 * time.Second,
		tail:   100000,
		now:    time.Now,
	}
	for _, o := range opts {
		o(t)
	}
	return t
}

func (t *Tracker) Players(instanceNumber int) (int, bool) {
	if t == nil {
		return 0, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	w, ok := t.state[instanceNumber]
	if !ok || !w.live {
		return 0, false
	}
	if t.now().Sub(w.connectedAt) < t.settle {
		return 0, false
	}
	return len(w.online), true
}

func (t *Tracker) Watch(ctx context.Context, instanceNumber int, s Streamer) {
	if t == nil || s == nil {
		return
	}
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			if err := t.consume(ctx, instanceNumber, s); err != nil && ctx.Err() == nil {
				slog.Debug("occupancy: log stream ended", "instance", instanceNumber, "err", err)
			}
			t.drop(instanceNumber)
			select {
			case <-ctx.Done():
				return
			case <-time.After(t.retry):
			}
		}
	}()
}

func (t *Tracker) consume(ctx context.Context, instanceNumber int, s Streamer) error {
	rc, err := s.StreamLogs(ctx, t.tail)
	if err != nil {
		return err
	}
	defer rc.Close()

	t.begin(instanceNumber)

	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		id, ev := domain.ValheimConnection(sc.Text())
		switch ev {
		case domain.ConnJoined:
			t.set(instanceNumber, id, true)
		case domain.ConnLeft:
			t.set(instanceNumber, id, false)
		}
	}
	return sc.Err()
}

func (t *Tracker) begin(instanceNumber int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state[instanceNumber] = &world{online: map[string]struct{}{}, connectedAt: t.now(), live: true}
}

func (t *Tracker) drop(instanceNumber int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if w, ok := t.state[instanceNumber]; ok {
		w.live = false
	}
}

func (t *Tracker) set(instanceNumber int, id string, online bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	w, ok := t.state[instanceNumber]
	if !ok {
		return
	}
	if online {
		w.online[id] = struct{}{}
		return
	}
	delete(w.online, id)
}
