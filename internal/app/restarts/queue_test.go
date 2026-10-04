package restarts

import (
	"context"
	"errors"
	"testing"
	"time"

	"agrelha/internal/domain"
)

type rig struct {
	q        *Queue
	rolled   []int
	players  int
	known    bool
	restartE error
	events   []string
	started  time.Time
	startOK  bool
}

func newRig(t *testing.T, opts ...Option) *rig {
	t.Helper()
	r := &rig{known: true}
	base := []Option{
		WithLookup(func(_ context.Context, num int) (*domain.Instance, error) {
			return &domain.Instance{GameID: domain.GameValheim, Number: num, Slug: "boppo"}, nil
		}),
		WithOccupancy(func(context.Context, domain.Instance) (int, bool) { return r.players, r.known }),
		WithRestarter(func(_ context.Context, inst domain.Instance) error {
			if r.restartE != nil {
				return r.restartE
			}
			r.rolled = append(r.rolled, inst.Number)
			return nil
		}),
		WithEvent(func(kind, detail string) { r.events = append(r.events, kind+":"+detail) }),
		WithStartedAt(func(context.Context, domain.Instance) (time.Time, bool) { return r.started, r.startOK }),
	}
	r.q = New(append(base, opts...)...)
	return r
}

func TestAnEmptyWorldIsRestarted(t *testing.T) {
	r := newRig(t)
	r.players, r.known = 0, true
	r.q.Request(2, "boppo", "config change")

	r.q.tick(context.Background())

	if len(r.rolled) != 1 || r.rolled[0] != 2 {
		t.Fatalf("expected instance 2 to be restarted, got %v", r.rolled)
	}
	if _, still := r.q.Pending(2); still {
		t.Error("a completed restart must leave the queue")
	}
	if len(r.events) != 1 {
		t.Errorf("the restart should be recorded, got %v", r.events)
	}
}

func TestAnOccupiedWorldWaitsAndSaysWhy(t *testing.T) {
	r := newRig(t)
	r.players, r.known = 3, true
	r.q.Request(2, "boppo", "config change")

	r.q.tick(context.Background())

	if len(r.rolled) != 0 {
		t.Fatal("a world with players on it must not be restarted")
	}
	p, ok := r.q.Pending(2)
	if !ok {
		t.Fatal("the restart should still be pending")
	}
	if !p.Waiting() {
		t.Error("a held restart should report itself as waiting")
	}
	if p.Blocked == "" || !contains(p.Blocked, "3 player") {
		t.Errorf("the reason should name the players, got %q", p.Blocked)
	}
}

func TestUnknownOccupancyIsNotTreatedAsEmpty(t *testing.T) {
	r := newRig(t)
	r.known = false
	r.q.Request(2, "boppo", "config change")

	r.q.tick(context.Background())

	if len(r.rolled) != 0 {
		t.Fatal("must not restart when occupancy is unknown")
	}
	p, _ := r.q.Pending(2)
	if !contains(p.Blocked, "cannot tell") {
		t.Errorf("the block reason should admit it does not know, got %q", p.Blocked)
	}
}

func TestNoOccupancySourceStillWaits(t *testing.T) {
	q := New(
		WithLookup(func(_ context.Context, num int) (*domain.Instance, error) {
			return &domain.Instance{Number: num}, nil
		}),
		WithRestarter(func(context.Context, domain.Instance) error {
			t.Error("must not restart with no way to check occupancy")
			return nil
		}),
	)
	q.Request(1, "x", "change")
	q.tick(context.Background())
	if p, ok := q.Pending(1); !ok || !p.Waiting() {
		t.Error("expected the restart to be held")
	}
}

func TestForceRestartsWithPlayersOnline(t *testing.T) {
	r := newRig(t)
	r.players, r.known = 5, true
	r.q.Request(2, "boppo", "config change")

	if err := r.q.Force(context.Background(), 2); err != nil {
		t.Fatalf("force failed: %v", err)
	}
	if len(r.rolled) != 1 {
		t.Error("force should have restarted regardless of players")
	}
	if _, still := r.q.Pending(2); still {
		t.Error("a forced restart must clear the pending entry")
	}
}

func TestForceWorksWithNothingPending(t *testing.T) {
	r := newRig(t)
	if err := r.q.Force(context.Background(), 7); err != nil {
		t.Fatalf("a manual restart should not require a pending change: %v", err)
	}
	if len(r.rolled) != 1 {
		t.Error("expected the restart to happen")
	}
}

func TestAFailedRestartStaysPending(t *testing.T) {
	r := newRig(t)
	r.restartE = errors.New("api down")
	r.q.Request(2, "boppo", "config change")

	r.q.tick(context.Background())

	p, ok := r.q.Pending(2)
	if !ok {
		t.Fatal("a failed restart must not be dropped from the queue")
	}
	if !contains(p.Blocked, "api down") {
		t.Errorf("the failure should be visible, got %q", p.Blocked)
	}
	if err := r.q.Force(context.Background(), 2); err == nil {
		t.Error("force should surface the underlying failure")
	}
}

func TestRequestingTwiceKeepsTheOriginalTimestamp(t *testing.T) {
	r := newRig(t)
	start := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	r.q.now = func() time.Time { return start }
	r.q.Request(2, "boppo", "first change")

	r.q.now = func() time.Time { return start.Add(time.Hour) }
	r.q.Request(2, "boppo", "second change")

	p, _ := r.q.Pending(2)
	if !p.Since.Equal(start) {
		t.Errorf("Since moved to %v; it should still be %v", p.Since, start)
	}
	if p.Reason != "second change" {
		t.Errorf("the newest reason should win, got %q", p.Reason)
	}
}

func TestCancelDropsWithoutRestarting(t *testing.T) {
	r := newRig(t)
	r.q.Request(2, "boppo", "config change")
	r.q.Cancel(2)
	r.q.tick(context.Background())
	if len(r.rolled) != 0 {
		t.Error("a cancelled restart must not happen")
	}
	if _, ok := r.q.Pending(2); ok {
		t.Error("expected the entry to be gone")
	}
}

func TestAllListsOldestFirst(t *testing.T) {
	r := newRig(t)
	base := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	r.q.now = func() time.Time { return base.Add(2 * time.Hour) }
	r.q.Request(3, "c", "later")
	r.q.now = func() time.Time { return base }
	r.q.Request(1, "a", "earlier")

	all := r.q.All()
	if len(all) != 2 || all[0].Number != 1 {
		t.Errorf("expected the oldest first, got %+v", all)
	}
}

func TestAMissingWorldIsReportedNotRestarted(t *testing.T) {
	r := newRig(t, WithLookup(func(context.Context, int) (*domain.Instance, error) {
		return nil, errors.New("gone")
	}))
	r.q.Request(9, "ghost", "config change")
	r.q.tick(context.Background())
	if len(r.rolled) != 0 {
		t.Error("nothing to restart")
	}
	if p, _ := r.q.Pending(9); !contains(p.Blocked, "not found") {
		t.Errorf("expected a not-found block reason, got %q", p.Blocked)
	}
}

func TestNilQueueIsInert(t *testing.T) {
	var q *Queue
	q.Request(1, "x", "y")
	q.Cancel(1)
	if _, ok := q.Pending(1); ok {
		t.Error("a nil queue has nothing pending")
	}
	if len(q.All()) != 0 {
		t.Error("a nil queue lists nothing")
	}
	if err := q.Force(context.Background(), 1); err == nil {
		t.Error("forcing on a nil queue should report unavailable")
	}
	q.Run(context.Background())
}

func TestRunAppliesOnItsInterval(t *testing.T) {
	r := newRig(t, WithInterval(5*time.Millisecond))
	r.players, r.known = 0, true
	r.q.Request(2, "boppo", "config change")

	ctx := t.Context()
	r.q.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, still := r.q.Pending(2); !still {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("Run did not apply the pending restart")
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 ||
		indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

func TestARestartByAnyOtherMeansClearsThePending(t *testing.T) {
	r := newRig(t)
	queued := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	r.q.now = func() time.Time { return queued }
	r.q.Request(2, "boppo", "config change")

	r.players, r.known = 5, true
	r.started, r.startOK = queued.Add(time.Minute), true

	r.q.tick(context.Background())

	if _, still := r.q.Pending(2); still {
		t.Error("the world restarted after the change was queued, so it has been applied")
	}
	if len(r.rolled) != 0 {
		t.Error("it must not restart again on top of that")
	}
}

func TestAnOlderRestartDoesNotCountAsApplied(t *testing.T) {
	r := newRig(t)
	queued := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	r.q.now = func() time.Time { return queued }
	r.q.Request(2, "boppo", "config change")

	r.players, r.known = 0, true
	r.started, r.startOK = queued.Add(-time.Hour), true

	r.q.tick(context.Background())

	if len(r.rolled) != 1 {
		t.Error("a pod that started before the change still needs restarting")
	}
}

func TestUnknownStartTimeDoesNotClearThePending(t *testing.T) {
	r := newRig(t)
	r.q.Request(2, "boppo", "config change")
	r.known, r.startOK = false, false

	r.q.tick(context.Background())

	if _, still := r.q.Pending(2); !still {
		t.Error("not knowing when the pod started is not evidence that it restarted")
	}
}

func TestSettleClearsWithoutRestartingAnything(t *testing.T) {
	r := newRig(t)
	queued := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	r.q.now = func() time.Time { return queued }
	r.q.Request(2, "boppo", "config change")
	r.started, r.startOK = queued.Add(time.Minute), true

	r.q.Settle(context.Background())

	if _, still := r.q.Pending(2); still {
		t.Error("Settle should drop an already-applied change")
	}
	if len(r.rolled) != 0 {
		t.Error("Settle must never restart")
	}
}
