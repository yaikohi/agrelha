package modupdates

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type fakeCatalog struct {
	latest map[string]string
	tree   map[string][]string
	down   map[string]bool

	mu    sync.Mutex
	calls int
	trees int
}

func (f *fakeCatalog) LatestVersion(_ context.Context, ns, name string) (string, []string, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	key := ns + "/" + name
	if f.down[key] {
		return "", nil, fmt.Errorf("dial tcp: i/o timeout")
	}
	v, ok := f.latest[key]
	if !ok {
		return "", nil, fmt.Errorf("%s: %w", key, ports.ErrPackageNotFound)
	}
	return v, nil, nil
}

func (f *fakeCatalog) ResolveTree(_ context.Context, ns, name string) ([]string, error) {
	f.mu.Lock()
	f.trees++
	f.mu.Unlock()
	out, ok := f.tree[ns+"/"+name]
	if !ok {
		return nil, fmt.Errorf("no such package")
	}
	return out, nil
}

type fakeInstances struct {
	insts   []domain.Instance
	entries map[int][]string
	wrote   map[int][]string
	details map[int]string
	err     error
}

func (f *fakeInstances) ListInstances(context.Context) ([]domain.Instance, error) {
	return f.insts, nil
}

func (f *fakeInstances) GetInstalledMods(_ context.Context, num int) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.entries[num], nil
}

func (f *fakeInstances) ReplaceMods(_ context.Context, num int, entries []string, detail string, _ ...string) (bool, error) {
	if f.wrote == nil {
		f.wrote = map[int][]string{}
		f.details = map[int]string{}
	}
	f.wrote[num] = entries
	f.details[num] = detail
	return true, nil
}

type fakeRestore struct {
	points map[int]*domain.ModRestorePoint
}

func (f *fakeRestore) SaveRestorePoint(_ domain.GameID, number int, previous, applied []string) error {
	if f.points == nil {
		f.points = map[int]*domain.ModRestorePoint{}
	}
	f.points[number] = &domain.ModRestorePoint{Previous: previous, Applied: applied}
	return nil
}

func (f *fakeRestore) RestorePoint(_ domain.GameID, number int) (*domain.ModRestorePoint, error) {
	return f.points[number], nil
}

func (f *fakeRestore) ClearRestorePoint(_ domain.GameID, number int) error {
	delete(f.points, number)
	return nil
}

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.2.1", "1.2.0", true},
		{"1.2.0", "1.2.0", false},
		{"1.2.0", "1.2.1", false},
		{"1.10.0", "1.9.0", true},
		{"2.0.0", "1.99.99", true},
		{"1.2", "1.2.0", false},
		{"1.2.0.1", "1.2.0", true},
	}
	for _, c := range cases {
		if got := domain.VersionNewer(c.a, c.b); got != c.want {
			t.Errorf("VersionNewer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// The regression this whole design exists for: the bulk index was hours stale
// and reported "up to date" for four mods that had newer versions. Asking about
// each mod by name is the only answer that cannot go quietly out of date.
func TestRefreshOneAsksAboutEveryInstalledMod(t *testing.T) {
	insts := &fakeInstances{entries: map[int][]string{
		2: {
			"denikson/BepInExPack_Valheim/5.4.2202",
			"MaddCatter365/Hearthkeeper/0.1.7",
			"MaddCatter365/WayfarerRecall/1.0.1",
			"ArgusMagnus-ServersideQoL-2.0.8",
			"Skarif-AutoRepairBuilding-1.2.15",
			"Neobotics/SlayerSkills/1.2.0",
		},
	}}
	cat := &fakeCatalog{latest: map[string]string{
		"denikson/BepInExPack_Valheim": "5.4.2202",
		"MaddCatter365/Hearthkeeper":   "0.1.9",
		"MaddCatter365/WayfarerRecall": "1.0.2",
		"ArgusMagnus/ServersideQoL":    "2.0.9",
		"Skarif/AutoRepairBuilding":    "1.2.16",
		"Neobotics/SlayerSkills":       "1.2.0",
	}}

	rep, err := New(insts, cat).RefreshOne(context.Background(), 2)
	if err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}
	if cat.calls != 6 {
		t.Errorf("asked upstream %d times, want one per installed mod (6)", cat.calls)
	}
	if rep.Checked != 6 || rep.Total != 6 {
		t.Errorf("checked %d of %d, want 6 of 6", rep.Checked, rep.Total)
	}

	var got []string
	for _, u := range rep.Updates {
		got = append(got, fmt.Sprintf("%s %s->%s", u.Ref.FullName(), u.Current, u.Latest))
	}
	want := []string{
		"ArgusMagnus-ServersideQoL 2.0.8->2.0.9",
		"MaddCatter365-Hearthkeeper 0.1.7->0.1.9",
		"MaddCatter365-WayfarerRecall 1.0.1->1.0.2",
		"Skarif-AutoRepairBuilding 1.2.15->1.2.16",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("updates = %v, want %v", got, want)
	}
}

// A mod that no longer exists upstream is not "up to date" and not a transient
// failure: the next boot cannot fetch it, so it gets its own bucket.
func TestMissingUpstreamIsNotSilentlySkipped(t *testing.T) {
	insts := &fakeInstances{entries: map[int][]string{
		1: {"Smoothbrain/Mining/1.3.4", "Deleted/Package/1.0.0", "Flaky/Thing/1.0.0"},
	}}
	cat := &fakeCatalog{
		latest: map[string]string{"Smoothbrain/Mining": "1.3.4", "Flaky/Thing": "2.0.0"},
		down:   map[string]bool{"Flaky/Thing": true},
	}

	rep, err := New(insts, cat).RefreshOne(context.Background(), 1)
	if err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}
	if len(rep.Missing) != 1 || rep.Missing[0].FullName() != "Deleted-Package" {
		t.Errorf("Missing = %v, want [Deleted-Package]", rep.Missing)
	}
	if len(rep.Unreachable) != 1 || rep.Unreachable[0].FullName() != "Flaky-Thing" {
		t.Errorf("Unreachable = %v, want [Flaky-Thing]", rep.Unreachable)
	}
	if rep.Checked != 1 || rep.Total != 3 {
		t.Errorf("checked %d of %d, want 1 of 3", rep.Checked, rep.Total)
	}
	if len(rep.Updates) != 0 {
		t.Errorf("an unreachable mod must not be reported as an update: %v", rep.Updates)
	}
}

// An unpinned entry has no version to compare against, so it can never be
// reported as out of date: doing so would offer an update that is a no-op.
func TestUnpinnedEntriesAreNotUpdates(t *testing.T) {
	insts := &fakeInstances{entries: map[int][]string{1: {"Smoothbrain-Mining"}}}
	cat := &fakeCatalog{latest: map[string]string{"Smoothbrain/Mining": "1.3.9"}}

	rep, err := New(insts, cat).RefreshOne(context.Background(), 1)
	if err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}
	if len(rep.Updates) != 0 {
		t.Fatalf("want no updates for an unpinned entry, got %+v", rep.Updates)
	}
	if rep.Checked != 1 {
		t.Errorf("the mod was still checked, want Checked=1, got %d", rep.Checked)
	}
}

func TestApplyRepinsTargetAndKeepsTheRest(t *testing.T) {
	insts := &fakeInstances{entries: map[int][]string{
		1: {
			"denikson/BepInExPack_Valheim/5.4.2202",
			"Smoothbrain/Mining/1.3.4",
			"blacks7ar/BowPlugin/1.8.7",
		},
	}}
	cat := &fakeCatalog{
		latest: map[string]string{
			"denikson/BepInExPack_Valheim": "5.4.2202",
			"Smoothbrain/Mining":           "1.3.9",
			"blacks7ar/BowPlugin":          "1.9.0",
		},
		tree: map[string][]string{"Smoothbrain/Mining": {"Smoothbrain/Mining/1.3.9"}},
	}

	c := New(insts, cat)
	if _, err := c.RefreshOne(context.Background(), 1); err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}
	applied, err := c.Apply(context.Background(), 1, []string{"smoothbrain-mining"}, "tester")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("want 1 applied, got %d", len(applied))
	}

	want := []string{
		"denikson/BepInExPack_Valheim/5.4.2202",
		"Smoothbrain/Mining/1.3.9",
		"blacks7ar/BowPlugin/1.8.7",
	}
	if got := insts.wrote[1]; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("wrote %v, want %v", got, want)
	}
	if got := insts.details[1]; got != "Smoothbrain-Mining 1.3.4 → 1.3.9" {
		t.Errorf("history detail = %q, want the version transition", got)
	}
}

func TestApplyAddsAndBumpsDependencies(t *testing.T) {
	insts := &fakeInstances{entries: map[int][]string{
		1: {"Smoothbrain/Mining/1.3.4", "Shared/Lib/1.0.0"},
	}}
	cat := &fakeCatalog{
		latest: map[string]string{"Smoothbrain/Mining": "1.3.9", "Shared/Lib": "1.0.0"},
		tree: map[string][]string{
			"Smoothbrain/Mining": {"Smoothbrain/Mining/1.3.9", "Shared/Lib/2.0.0", "New/Dependency/0.1.0"},
		},
	}

	c := New(insts, cat)
	if _, err := c.RefreshOne(context.Background(), 1); err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}
	if _, err := c.Apply(context.Background(), 1, nil, "tester"); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	want := []string{"Smoothbrain/Mining/1.3.9", "Shared/Lib/2.0.0", "New/Dependency/0.1.0"}
	if got := insts.wrote[1]; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("wrote %v, want %v", got, want)
	}
}

// A dependency already pinned above what the target asks for is left alone:
// updating one mod must never silently roll another one back.
func TestApplyNeverDowngradesADependency(t *testing.T) {
	insts := &fakeInstances{entries: map[int][]string{
		1: {"Smoothbrain/Mining/1.3.4", "Shared/Lib/3.0.0"},
	}}
	cat := &fakeCatalog{
		latest: map[string]string{"Smoothbrain/Mining": "1.3.9", "Shared/Lib": "3.0.0"},
		tree:   map[string][]string{"Smoothbrain/Mining": {"Smoothbrain/Mining/1.3.9", "Shared/Lib/2.0.0"}},
	}

	c := New(insts, cat)
	if _, err := c.RefreshOne(context.Background(), 1); err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}
	if _, err := c.Apply(context.Background(), 1, nil, "tester"); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	want := []string{"Smoothbrain/Mining/1.3.9", "Shared/Lib/3.0.0"}
	if got := insts.wrote[1]; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("wrote %v, want %v", got, want)
	}
}

func TestApplyRefusesUnknownSelection(t *testing.T) {
	insts := &fakeInstances{entries: map[int][]string{1: {"Smoothbrain/Mining/1.3.4"}}}
	cat := &fakeCatalog{latest: map[string]string{"Smoothbrain/Mining": "1.3.9"}}

	c := New(insts, cat)
	if _, err := c.RefreshOne(context.Background(), 1); err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}
	if _, err := c.Apply(context.Background(), 1, []string{"someone-else"}, "tester"); err == nil {
		t.Fatal("want an error when nothing selected has an update, got nil")
	}
	if cat.trees != 0 {
		t.Errorf("resolved %d trees for a selection with no updates, want 0", cat.trees)
	}
}

func TestPendingClearsOnceTheWorldReportsTheNewPins(t *testing.T) {
	insts := &fakeInstances{entries: map[int][]string{1: {"Smoothbrain/Mining/1.3.4"}}}
	cat := &fakeCatalog{
		latest: map[string]string{"Smoothbrain/Mining": "1.3.9"},
		tree:   map[string][]string{"Smoothbrain/Mining": {"Smoothbrain/Mining/1.3.9"}},
	}

	ctx := context.Background()
	c := New(insts, cat)
	if _, err := c.RefreshOne(ctx, 1); err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}
	if _, err := c.Apply(ctx, 1, nil, "tester"); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if !c.Pending(ctx, 1) {
		t.Fatal("want pending while the world still reports the old pin")
	}
	if c.Count(1) != 0 {
		t.Errorf("badge should stop counting an update already ordered, got %d", c.Count(1))
	}

	insts.entries[1] = []string{"Smoothbrain/Mining/1.3.9"}
	if c.Pending(ctx, 1) {
		t.Error("want pending cleared once the world reports the new pin")
	}
}

func TestUndoRestoresThePreviousPins(t *testing.T) {
	insts := &fakeInstances{entries: map[int][]string{
		1: {"Smoothbrain/Mining/1.3.4", "Shared/Lib/1.0.0"},
	}}
	cat := &fakeCatalog{
		latest: map[string]string{"Smoothbrain/Mining": "1.3.9", "Shared/Lib": "1.0.0"},
		tree:   map[string][]string{"Smoothbrain/Mining": {"Smoothbrain/Mining/1.3.9"}},
	}
	rs := &fakeRestore{}

	ctx := context.Background()
	c := New(insts, cat, WithRestorePoints(rs))
	if _, err := c.RefreshOne(ctx, 1); err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}
	if _, err := c.Apply(ctx, 1, nil, "tester"); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	insts.entries[1] = insts.wrote[1]
	if _, err := c.Undo(ctx, 1, "tester"); err != nil {
		t.Fatalf("Undo: %v", err)
	}

	want := []string{"Smoothbrain/Mining/1.3.4", "Shared/Lib/1.0.0"}
	if got := insts.wrote[1]; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("undo wrote %v, want %v", got, want)
	}
	if rs.points[1] != nil {
		t.Error("one step back, not a stack: using the restore point must consume it")
	}
}

// Something else touched the mod list after the update, so reverting would
// silently discard it. The way back is withdrawn rather than misapplied.
func TestUndoWithdrawnWhenTheListMovedOn(t *testing.T) {
	insts := &fakeInstances{entries: map[int][]string{1: {"Smoothbrain/Mining/1.3.4"}}}
	cat := &fakeCatalog{
		latest: map[string]string{"Smoothbrain/Mining": "1.3.9"},
		tree:   map[string][]string{"Smoothbrain/Mining": {"Smoothbrain/Mining/1.3.9"}},
	}
	rs := &fakeRestore{}

	ctx := context.Background()
	c := New(insts, cat, WithRestorePoints(rs))
	if _, err := c.RefreshOne(ctx, 1); err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}
	if _, err := c.Apply(ctx, 1, nil, "tester"); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	insts.entries[1] = append(append([]string{}, insts.wrote[1]...), "Someone/Else/1.0.0")

	rp, err := c.RestoreAvailable(ctx, 1)
	if err != nil {
		t.Fatalf("RestoreAvailable: %v", err)
	}
	if rp != nil {
		t.Fatal("want no restore point once the list has changed since the update")
	}
	if _, err := c.Undo(ctx, 1, "tester"); err == nil {
		t.Fatal("want Undo refused once the list has changed since the update")
	}
}

func TestRefreshSkipsVanillaWorlds(t *testing.T) {
	insts := &fakeInstances{
		insts:   []domain.Instance{{Number: 1, GameID: domain.GameValheim, Source: domain.SourceVanilla}},
		entries: map[int][]string{1: {"Smoothbrain/Mining/1.3.4"}},
	}
	cat := &fakeCatalog{latest: map[string]string{"Smoothbrain/Mining": "1.3.9"}}

	c := New(insts, cat)
	c.Refresh(context.Background())
	if got := c.Count(1); got != 0 {
		t.Errorf("a Vanilla world has no mods to update, got %d", got)
	}
	if cat.calls != 0 {
		t.Errorf("asked upstream %d times about a Vanilla world, want 0", cat.calls)
	}
}
