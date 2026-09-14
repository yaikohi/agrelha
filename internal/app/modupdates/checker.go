// Package modupdates answers "is anything installed on this world out of date?"
// It asks the upstream catalogue about each installed mod by name, which is the
// only authoritative answer — a bulk index is cheap but can be hours behind, and
// a stale "everything is fine" is worse than no answer at all. Because that costs
// one request per mod, the question is asked on a ticker and page renders read
// the resulting snapshot.
package modupdates

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

const (
	defaultInterval = 15 * time.Minute
	pendingTTL      = 6 * time.Minute
	maxConcurrent   = 6
)

// Catalog resolves the current version of one package upstream.
type Catalog interface {
	LatestVersion(ctx context.Context, ns, name string) (string, []string, error)
	ResolveTree(ctx context.Context, ns, name string) ([]string, error)
}

// InstanceCatalog resolves versions and dependencies using world context (MC version, loader).
type InstanceCatalog interface {
	LatestVersionForInstance(ctx context.Context, ref domain.ModRef, inst domain.Instance) (string, []string, error)
	ResolveTreeForInstance(ctx context.Context, ref domain.ModRef, inst domain.Instance) ([]string, error)
}

// Instances is the set of worlds the checker watches, and the way it rewrites
// the mod list of one of them.
type Instances interface {
	ListInstances(ctx context.Context) ([]domain.Instance, error)
	GetInstalledMods(ctx context.Context, num int) ([]string, error)
	ReplaceMods(ctx context.Context, num int, entries []string, detail string, actor ...string) (bool, error)
}

// RestorePoints persists the way back from an update.
type RestorePoints interface {
	SaveRestorePoint(game domain.GameID, number int, previous, applied []string) error
	RestorePoint(game domain.GameID, number int) (*domain.ModRestorePoint, error)
	ClearRestorePoint(game domain.GameID, number int) error
}

// Report is one world's answer, including what could not be answered. "Up to
// date", "could not ask" and "no longer published" are three different things
// and are never collapsed into one.
type Report struct {
	Updates     []domain.ModUpdate
	Missing     []domain.ModRef
	Unreachable []domain.ModRef
	Checked     int
	Total       int
	At          time.Time
}

type snapshot struct {
	report Report
	err    error
}

type pending struct {
	want map[string]bool
	at   time.Time
}

// Checker keeps a per-Instance Report, applies the updates the operator picks,
// and holds the single step back from the last apply.
type Checker struct {
	insts    Instances
	cat      Catalog
	restore  RestorePoints
	gameID   domain.GameID
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

// WithRestorePoints enables undo. Without it, updates still apply; they just
// cannot be reverted from the UI.
func WithRestorePoints(r RestorePoints) Option {
	return func(c *Checker) { c.restore = r }
}

// WithGameID sets the game ID the checker watches (default GameValheim).
func WithGameID(id domain.GameID) Option {
	return func(c *Checker) { c.gameID = id }
}

// New builds a Checker over the given worlds and catalogue.
func New(insts Instances, cat Catalog, opts ...Option) *Checker {
	c := &Checker{
		insts:    insts,
		cat:      cat,
		gameID:   domain.GameValheim,
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

// Refresh recomputes the report for every Modded world.
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
		if inst.IsVanilla() || inst.PackDefined() {
			continue
		}
		if _, err := c.RefreshOne(ctx, inst.Number); err != nil {
			slog.Warn("modupdates: refresh failed", "instance", inst.Number, "err", err)
		}
	}
}

// RefreshOne recomputes the report for one world and returns it.
func (c *Checker) RefreshOne(ctx context.Context, num int) (Report, error) {
	if c == nil || c.insts == nil || c.cat == nil {
		return Report{}, nil
	}
	rep, err := c.compute(ctx, num)
	c.mu.Lock()
	c.snap[num] = snapshot{report: rep, err: err}
	c.mu.Unlock()
	if len(rep.Missing) > 0 {
		slog.Warn("modupdates: installed mods are no longer published upstream",
			"instance", num, "count", len(rep.Missing), "first", rep.Missing[0].FullName())
	}
	return rep, err
}

func (c *Checker) getInstance(ctx context.Context, num int) (domain.Instance, error) {
	if ig, ok := c.insts.(interface {
		GetInstance(context.Context, int) (*domain.Instance, error)
	}); ok {
		inst, err := ig.GetInstance(ctx, num)
		if err == nil && inst != nil {
			return *inst, nil
		}
	}
	all, err := c.insts.ListInstances(ctx)
	if err != nil {
		return domain.Instance{Number: num, GameID: c.gameID}, nil
	}
	for _, inst := range all {
		if inst.Number == num {
			return inst, nil
		}
	}
	return domain.Instance{Number: num, GameID: c.gameID}, nil
}

func (c *Checker) latestVersionForRef(ctx context.Context, ref domain.ModRef, inst domain.Instance) (string, []string, error) {
	if ic, ok := c.cat.(InstanceCatalog); ok {
		return ic.LatestVersionForInstance(ctx, ref, inst)
	}
	if c.cat != nil {
		return c.cat.LatestVersion(ctx, ref.Namespace, ref.Name)
	}
	return "", nil, nil
}

func (c *Checker) resolveTreeForRef(ctx context.Context, ref domain.ModRef, inst domain.Instance) ([]string, error) {
	if ic, ok := c.cat.(InstanceCatalog); ok {
		return ic.ResolveTreeForInstance(ctx, ref, inst)
	}
	if c.cat != nil {
		return c.cat.ResolveTree(ctx, ref.Namespace, ref.Name)
	}
	return nil, nil
}

func (c *Checker) compute(ctx context.Context, num int) (Report, error) {
	inst, err := c.getInstance(ctx, num)
	if err != nil {
		return Report{}, err
	}
	if inst.IsVanilla() || inst.PackDefined() {
		return Report{At: time.Now()}, nil
	}

	entries, err := c.insts.GetInstalledMods(ctx, num)
	if err != nil {
		return Report{}, err
	}

	var refs []domain.ModRef
	for _, e := range entries {
		ref, ok := domain.ParseModRef(e, c.gameID)
		if !ok {
			continue
		}
		if c.gameID == domain.GameValheim && ref.Namespace == "" {
			continue
		}
		refs = append(refs, ref)
	}

	rep := Report{Total: len(refs), At: time.Now()}
	if len(refs) == 0 {
		return rep, nil
	}

	type result struct {
		ref    domain.ModRef
		latest string
		err    error
	}
	results := make([]result, len(refs))

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for i, ref := range refs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			v, _, err := c.latestVersionForRef(ctx, ref, inst)
			results[i] = result{ref: ref, latest: v, err: err}
		}()
	}
	wg.Wait()

	for _, r := range results {
		switch {
		case errors.Is(r.err, ports.ErrPackageNotFound):
			rep.Missing = append(rep.Missing, r.ref)
		case r.err != nil:
			rep.Unreachable = append(rep.Unreachable, r.ref)
		default:
			rep.Checked++
			if r.latest == "" {
				continue
			}
			if c.gameID == domain.GameValheim && r.ref.Version == "" {
				continue
			}
			currentVer := r.ref.Version
			if currentVer == "" {
				currentVer = "(unpinned)"
			}
			if domain.VersionNewer(r.latest, currentVer) {
				rep.Updates = append(rep.Updates, domain.ModUpdate{
					Ref: r.ref, Current: currentVer, Latest: r.latest,
				})
			}
		}
	}

	sort.Slice(rep.Updates, func(i, j int) bool { return rep.Updates[i].Ref.Key() < rep.Updates[j].Ref.Key() })
	sort.Slice(rep.Missing, func(i, j int) bool { return rep.Missing[i].Key() < rep.Missing[j].Key() })
	sort.Slice(rep.Unreachable, func(i, j int) bool { return rep.Unreachable[i].Key() < rep.Unreachable[j].Key() })
	return rep, nil
}

// Report returns the last computed answer for one world.
func (c *Checker) Report(num int) Report {
	if c == nil {
		return Report{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap[num].report
}

// LastError is why the last check for one world failed, if it did.
func (c *Checker) LastError(num int) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap[num].err
}

// Updates is the update list from the last report.
func (c *Checker) Updates(num int) []domain.ModUpdate { return c.Report(num).Updates }

// Count is how many mods on one world have a newer version upstream.
func (c *Checker) Count(num int) int { return len(c.Report(num).Updates) }

// Counts is Count for every world the checker has seen.
func (c *Checker) Counts() map[int]int {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[int]int, len(c.snap))
	for num, s := range c.snap {
		out[num] = len(s.report.Updates)
	}
	return out
}

// Total is the number of pending updates across every world.
func (c *Checker) Total() int {
	n := 0
	for _, v := range c.Counts() {
		n += v
	}
	return n
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
	kept := s.report.Updates[:0:0]
	for _, u := range s.report.Updates {
		if !done[u.Ref.Key()] {
			kept = append(kept, u)
		}
	}
	s.report.Updates = kept
	c.snap[num] = s
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
		rep, err := c.RefreshOne(ctx, num)
		if err != nil {
			return nil, err
		}
		available = rep.Updates
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

	inst, err := c.getInstance(ctx, num)
	if err != nil {
		return nil, err
	}

	previous, err := c.insts.GetInstalledMods(ctx, num)
	if err != nil {
		return nil, err
	}

	order := make([]string, 0, len(previous))
	byKey := map[string]domain.ModRef{}
	for _, e := range previous {
		ref, ok := domain.ParseModRef(e, c.gameID)
		if !ok {
			continue
		}
		if _, seen := byKey[ref.Key()]; !seen {
			order = append(order, ref.Key())
		}
		byKey[ref.Key()] = ref
	}

	for _, t := range targets {
		resolved, err := c.resolveTreeForRef(ctx, t.Ref, inst)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve %s: %w", t.Ref.FullName(), err)
		}
		for _, r := range resolved {
			ref, ok := domain.ParseModRef(r, c.gameID)
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

	changed, err := c.insts.ReplaceMods(ctx, num, out, updateDetail(targets), actor)
	if err != nil {
		return nil, err
	}
	if changed {
		c.markPending(num, out)
		c.saveRestorePoint(num, previous, out)
	}
	c.forget(num, targets)
	return targets, nil
}

// Undo restores the mod list from before the last update. It refuses when the
// world's current list is not exactly what that update wrote: something else has
// changed the mods since, and reverting would silently discard it.
func (c *Checker) Undo(ctx context.Context, num int, actor string) (*domain.ModRestorePoint, error) {
	if c == nil || c.restore == nil {
		return nil, fmt.Errorf("no restore point is available")
	}
	rp, err := c.RestoreAvailable(ctx, num)
	if err != nil {
		return nil, err
	}
	if rp == nil {
		return nil, fmt.Errorf("no restore point is available")
	}

	changed, err := c.insts.ReplaceMods(ctx, num, rp.Previous, "Reverted the last mod update", actor)
	if err != nil {
		return nil, err
	}
	if changed {
		c.markPending(num, rp.Previous)
	}
	// One step back, not a stack: using the way back consumes it, so undo can
	// never ping-pong against redo.
	if err := c.restore.ClearRestorePoint(c.gameID, num); err != nil {
		slog.Warn("modupdates: cannot clear restore point", "instance", num, "err", err)
	}
	if _, err := c.RefreshOne(ctx, num); err != nil {
		slog.Warn("modupdates: refresh after undo failed", "instance", num, "err", err)
	}
	return rp, nil
}

// RestoreAvailable returns the restore point only while it is still safe to use,
// clearing it once it is not.
func (c *Checker) RestoreAvailable(ctx context.Context, num int) (*domain.ModRestorePoint, error) {
	if c == nil || c.restore == nil {
		return nil, nil
	}
	rp, err := c.restore.RestorePoint(c.gameID, num)
	if err != nil || rp == nil {
		return nil, err
	}
	entries, err := c.insts.GetInstalledMods(ctx, num)
	if err != nil {
		return nil, err
	}
	if !rp.Matches(entries) {
		if err := c.restore.ClearRestorePoint(c.gameID, num); err != nil {
			slog.Warn("modupdates: cannot clear stale restore point", "instance", num, "err", err)
		}
		return nil, nil
	}
	return rp, nil
}

func (c *Checker) saveRestorePoint(num int, previous, applied []string) {
	if c.restore == nil {
		return
	}
	if err := c.restore.SaveRestorePoint(c.gameID, num, previous, applied); err != nil {
		slog.Warn("modupdates: cannot save restore point", "instance", num, "err", err)
	}
}

func updateDetail(ups []domain.ModUpdate) string {
	parts := make([]string, 0, len(ups))
	for _, u := range ups {
		parts = append(parts, fmt.Sprintf("%s %s → %s", u.Ref.FullName(), u.Current, u.Latest))
	}
	return strings.Join(parts, ", ")
}
