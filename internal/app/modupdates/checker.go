// Package modupdates answers "is anything installed on this world out of date?"
// without making a page render pay for it. Reading an Instance's mod list costs
// a cluster or repository round trip, so the answer is refreshed on a ticker and
// served from a per-Instance snapshot.
package modupdates

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"agrelha/internal/domain"
)

const (
	defaultInterval = 15 * time.Minute
	pendingTTL      = 6 * time.Minute
)

// Catalog is the upstream package index, keyed by domain.ModRef.CatalogKey.
type Catalog interface {
	Get(catalogKey string) (domain.ModSearchResult, bool)
	ResolveTree(ctx context.Context, ns, name string) ([]string, error)
	Ready() bool
}

// Instances is the set of worlds the checker watches, and the way it rewrites
// the pins of one of them.
type Instances interface {
	ListInstances(ctx context.Context) ([]domain.Instance, error)
	GetInstalledMods(ctx context.Context, num int) ([]string, error)
	ReplaceMods(ctx context.Context, num int, entries []string, actor ...string) (bool, error)
}

type snapshot struct {
	updates []domain.ModUpdate
	at      time.Time
	err     error
}

type pending struct {
	want map[string]bool
	at   time.Time
}

// Checker keeps a per-Instance view of which installed mods have a newer
// version upstream, and applies the ones the operator picks.
type Checker struct {
	insts    Instances
	cat      Catalog
	interval time.Duration

	mu   sync.Mutex
	snap map[int]snapshot
	pend map[int]pending
}

// Option configures a Checker.
type Option func(*Checker)

// WithInterval overrides how often the background refresh runs.
func WithInterval(d time.Duration) Option {
	return func(c *Checker) {
		if d > 0 {
			c.interval = d
		}
	}
}

// New builds a Checker over the given worlds and package index.
func New(insts Instances, cat Catalog, opts ...Option) *Checker {
	c := &Checker{
		insts:    insts,
		cat:      cat,
		interval: defaultInterval,
		snap:     map[int]snapshot{},
		pend:     map[int]pending{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Start refreshes every world once, then keeps refreshing on the interval until
// ctx is cancelled.
func (c *Checker) Start(ctx context.Context) {
	if c == nil || c.insts == nil || c.cat == nil {
		return
	}
	go func() {
		c.Refresh(ctx)
		t := time.NewTicker(c.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				c.Refresh(ctx)
			}
		}
	}()
}

// Refresh recomputes the snapshot for every world.
func (c *Checker) Refresh(ctx context.Context) {
	if c == nil || c.insts == nil {
		return
	}
	insts, err := c.insts.ListInstances(ctx)
	if err != nil {
		slog.Warn("modupdates: cannot list instances", "err", err)
		return
	}
	for _, inst := range insts {
		if inst.IsVanilla() {
			continue
		}
		if _, err := c.RefreshOne(ctx, inst.Number); err != nil {
			slog.Warn("modupdates: refresh failed", "instance", inst.Number, "err", err)
		}
	}
}

// RefreshOne recomputes the snapshot for one world and returns it.
func (c *Checker) RefreshOne(ctx context.Context, num int) ([]domain.ModUpdate, error) {
	if c == nil || c.insts == nil || c.cat == nil {
		return nil, nil
	}
	ups, err := c.compute(ctx, num)
	c.mu.Lock()
	c.snap[num] = snapshot{updates: ups, at: time.Now(), err: err}
	c.mu.Unlock()
	return ups, err
}

func (c *Checker) compute(ctx context.Context, num int) ([]domain.ModUpdate, error) {
	if !c.cat.Ready() {
		return nil, fmt.Errorf("mod catalog is still indexing")
	}
	entries, err := c.insts.GetInstalledMods(ctx, num)
	if err != nil {
		return nil, err
	}
	var out []domain.ModUpdate
	for _, e := range entries {
		ref, ok := domain.ParseModRef(e, domain.GameValheim)
		if !ok || ref.Namespace == "" || ref.Version == "" {
			continue
		}
		latest, ok := c.cat.Get(ref.CatalogKey())
		if !ok || latest.Version == "" {
			continue
		}
		if domain.VersionNewer(latest.Version, ref.Version) {
			out = append(out, domain.ModUpdate{Ref: ref, Current: ref.Version, Latest: latest.Version})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref.Key() < out[j].Ref.Key() })
	return out, nil
}

// Updates returns the last computed updates for one world.
func (c *Checker) Updates(num int) []domain.ModUpdate {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap[num].updates
}

// CheckedAt reports when the world was last checked; the zero time means never.
func (c *Checker) CheckedAt(num int) time.Time {
	if c == nil {
		return time.Time{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap[num].at
}

// Count is how many mods on one world have a newer version upstream.
func (c *Checker) Count(num int) int {
	return len(c.Updates(num))
}

// Counts is Count for every world the checker has seen.
func (c *Checker) Counts() map[int]int {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[int]int, len(c.snap))
	for num, s := range c.snap {
		out[num] = len(s.updates)
	}
	return out
}

// Pending reports whether an applied update is still waiting for the declared
// state to reach the cluster. It clears itself once the world reports the
// versions that were written, or after pendingTTL.
func (c *Checker) Pending(ctx context.Context, num int) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	p, ok := c.pend[num]
	c.mu.Unlock()
	if !ok {
		return false
	}
	if time.Since(p.at) > pendingTTL {
		c.clearPending(num)
		return false
	}
	entries, err := c.insts.GetInstalledMods(ctx, num)
	if err != nil {
		return true
	}
	have := make(map[string]bool, len(entries))
	for _, e := range entries {
		have[e] = true
	}
	for e := range p.want {
		if !have[e] {
			return true
		}
	}
	c.clearPending(num)
	return false
}

// forget drops the updates that were just written, so the badge stops counting
// work the operator has already ordered. The next refresh re-derives the truth.
func (c *Checker) forget(num int, applied []domain.ModUpdate) {
	done := make(map[string]bool, len(applied))
	for _, u := range applied {
		done[u.Ref.Key()] = true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.snap[num]
	kept := s.updates[:0:0]
	for _, u := range s.updates {
		if !done[u.Ref.Key()] {
			kept = append(kept, u)
		}
	}
	s.updates = kept
	c.snap[num] = s
}

func (c *Checker) markPending(num int, entries []string) {
	want := make(map[string]bool, len(entries))
	for _, e := range entries {
		want[e] = true
	}
	c.mu.Lock()
	c.pend[num] = pending{want: want, at: time.Now()}
	c.mu.Unlock()
}

func (c *Checker) clearPending(num int) {
	c.mu.Lock()
	delete(c.pend, num)
	c.mu.Unlock()
}

// Apply repins the named mods to their latest version, re-resolving each one's
// dependency tree, and rewrites the world's mod list. Keys are ModRef.Key
// values; an empty list means every update the last check found. It returns the
// updates it wrote.
func (c *Checker) Apply(ctx context.Context, num int, keys []string, actor string) ([]domain.ModUpdate, error) {
	if c == nil || c.insts == nil || c.cat == nil {
		return nil, fmt.Errorf("mod updates unavailable")
	}

	available := c.Updates(num)
	if len(available) == 0 {
		var err error
		if available, err = c.RefreshOne(ctx, num); err != nil {
			return nil, err
		}
	}
	if len(available) == 0 {
		return nil, nil
	}

	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	var targets []domain.ModUpdate
	for _, u := range available {
		if len(keys) == 0 || want[u.Ref.Key()] {
			targets = append(targets, u)
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("none of the selected mods have an update")
	}

	entries, err := c.insts.GetInstalledMods(ctx, num)
	if err != nil {
		return nil, err
	}

	order := make([]string, 0, len(entries))
	byKey := map[string]domain.ModRef{}
	for _, e := range entries {
		ref, ok := domain.ParseModRef(e, domain.GameValheim)
		if !ok {
			continue
		}
		if _, seen := byKey[ref.Key()]; !seen {
			order = append(order, ref.Key())
		}
		byKey[ref.Key()] = ref
	}

	for _, t := range targets {
		resolved, err := c.cat.ResolveTree(ctx, t.Ref.Namespace, t.Ref.Name)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve %s: %w", t.Ref.FullName(), err)
		}
		for _, r := range resolved {
			ref, ok := domain.ParseModRef(r, domain.GameValheim)
			if !ok || ref.Version == "" {
				continue
			}
			cur, have := byKey[ref.Key()]
			switch {
			case !have:
				order = append(order, ref.Key())
				byKey[ref.Key()] = ref
			case ref.Key() == t.Ref.Key():
				byKey[ref.Key()] = ref
			case domain.VersionNewer(ref.Version, cur.Version):
				byKey[ref.Key()] = ref
			}
		}
		if cur := byKey[t.Ref.Key()]; cur.Version != t.Latest {
			cur.Version = t.Latest
			byKey[t.Ref.Key()] = cur
		}
	}

	out := make([]string, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k].Entry())
	}

	changed, err := c.insts.ReplaceMods(ctx, num, out, actor)
	if err != nil {
		return nil, err
	}
	if changed {
		c.markPending(num, out)
	}
	c.forget(num, targets)
	return targets, nil
}
