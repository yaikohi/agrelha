// Package restarts holds changes that only a restart will apply until the
// world is empty.
//
// Committing a config Override does not change anything a player can see: mods
// read their configuration once, at load. So the change sits in git, applied by
// the next pod start. Rolling immediately would disconnect whoever is playing
// over a setting they did not ask about; never rolling leaves the change
// silently unapplied for days. This waits for the world to empty, and says so
// while it waits.
package restarts

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"agrelha/internal/domain"
)

// Pending is a restart that a committed change requires.
type Pending struct {
	Number int
	Slug   string
	Reason string
	Since  time.Time
	// Blocked says why it has not happened yet: players online, or an
	// occupancy answer we do not have. Empty means it is ready to go.
	Blocked string
}

// Waiting reports whether this restart is being held back.
func (p Pending) Waiting() bool { return p.Blocked != "" }

// Occupancy reports the number of players on a world, and whether that number
// is known at all. Unknown is never treated as empty.
type Occupancy func(ctx context.Context, inst domain.Instance) (int, bool)

// Restarter rolls one world.
type Restarter func(ctx context.Context, inst domain.Instance) error

// InstanceLookup resolves an instance number to the Instance itself.
type InstanceLookup func(ctx context.Context, num int) (*domain.Instance, error)

// EventRecorder notes that a restart happened, for the activity log.
type EventRecorder func(kind, detail string)

type Option func(*Queue)

func WithOccupancy(fn Occupancy) Option    { return func(q *Queue) { q.players = fn } }
func WithRestarter(fn Restarter) Option    { return func(q *Queue) { q.restart = fn } }
func WithLookup(fn InstanceLookup) Option  { return func(q *Queue) { q.lookup = fn } }
func WithEvent(fn EventRecorder) Option    { return func(q *Queue) { q.event = fn } }
func WithInterval(d time.Duration) Option  { return func(q *Queue) { q.interval = d } }
func WithClock(fn func() time.Time) Option { return func(q *Queue) { q.now = fn } }

// Queue holds pending restarts and applies them when their world empties.
type Queue struct {
	mu      sync.Mutex
	pending map[int]Pending

	players  Occupancy
	restart  Restarter
	lookup   InstanceLookup
	event    EventRecorder
	interval time.Duration
	now      func() time.Time
}

func New(opts ...Option) *Queue {
	q := &Queue{
		pending:  map[int]Pending{},
		interval: 30 * time.Second,
		now:      time.Now,
	}
	for _, o := range opts {
		o(q)
	}
	return q
}

// Request records that an Instance needs a restart. Asking twice does not
// reset the clock: the operator wants to know how long the change has been
// waiting, not how recently they last edited something.
func (q *Queue) Request(num int, slug, reason string) {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if existing, ok := q.pending[num]; ok {
		existing.Reason = reason
		q.pending[num] = existing
		return
	}
	q.pending[num] = Pending{Number: num, Slug: slug, Reason: reason, Since: q.now()}
}

// Pending reports the restart waiting on one Instance, if any.
func (q *Queue) Pending(num int) (Pending, bool) {
	if q == nil {
		return Pending{}, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	p, ok := q.pending[num]
	return p, ok
}

// All lists every pending restart, oldest first.
func (q *Queue) All() []Pending {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Pending, 0, len(q.pending))
	for _, p := range q.pending {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	return out
}

// Cancel drops a pending restart without performing it.
func (q *Queue) Cancel(num int) {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.pending, num)
}

// Force restarts now, whoever is connected. This is the operator overruling the
// wait, so it does not consult occupancy at all.
func (q *Queue) Force(ctx context.Context, num int) error {
	if q == nil {
		return fmt.Errorf("restart queue unavailable")
	}
	q.mu.Lock()
	p, ok := q.pending[num]
	q.mu.Unlock()
	if !ok {
		p = Pending{Number: num, Reason: "manual restart"}
	}
	if err := q.roll(ctx, p); err != nil {
		return err
	}
	q.Cancel(num)
	return nil
}

// Run applies pending restarts as their worlds empty, until ctx is done.
func (q *Queue) Run(ctx context.Context) {
	if q == nil || q.restart == nil {
		return
	}
	go func() {
		t := time.NewTicker(q.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				q.tick(ctx)
			}
		}
	}()
}

func (q *Queue) tick(ctx context.Context) {
	for _, p := range q.All() {
		inst, err := q.resolve(ctx, p)
		if err != nil {
			q.block(p.Number, "world not found")
			continue
		}

		players, known := 0, false
		if q.players != nil {
			players, known = q.players(ctx, *inst)
		}
		switch {
		case !known:
			// A server that is down, starting, or unreachable is not an empty
			// one. Restarting on an unknown answer is how you disconnect people
			// during a netsplit, so it waits for the operator instead.
			q.block(p.Number, "cannot tell if anyone is playing")
			continue
		case players > 0:
			q.block(p.Number, fmt.Sprintf("%d player(s) online", players))
			continue
		}

		if err := q.roll(ctx, p); err != nil {
			q.block(p.Number, "restart failed: "+err.Error())
			continue
		}
		q.Cancel(p.Number)
	}
}

func (q *Queue) resolve(ctx context.Context, p Pending) (*domain.Instance, error) {
	if q.lookup == nil {
		return nil, fmt.Errorf("no instance lookup configured")
	}
	inst, err := q.lookup(ctx, p.Number)
	if err != nil || inst == nil {
		return nil, fmt.Errorf("instance %d not found", p.Number)
	}
	return inst, nil
}

func (q *Queue) roll(ctx context.Context, p Pending) error {
	if q.restart == nil {
		return fmt.Errorf("no restarter configured")
	}
	inst, err := q.resolve(ctx, p)
	if err != nil {
		return err
	}
	if err := q.restart(ctx, *inst); err != nil {
		return err
	}
	if q.event != nil {
		q.event("restart-applied", fmt.Sprintf("#%02d %s", p.Number, p.Reason))
	}
	return nil
}

func (q *Queue) block(num int, why string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if p, ok := q.pending[num]; ok {
		p.Blocked = why
		q.pending[num] = p
	}
}
