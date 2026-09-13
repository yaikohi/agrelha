package modupdates

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agrelha/internal/domain"
)

type fakeCatalog struct {
	latest  map[string]string
	tree    map[string][]string
	notdone bool
	calls   int
}

func (f *fakeCatalog) Get(key string) (domain.ModSearchResult, bool) {
	v, ok := f.latest[key]
	if !ok {
		return domain.ModSearchResult{}, false
	}
	ns, name, _ := strings.Cut(key, "/")
	return domain.ModSearchResult{Owner: ns, Name: name, Version: v}, true
}

func (f *fakeCatalog) ResolveTree(_ context.Context, ns, name string) ([]string, error) {
	f.calls++
	out, ok := f.tree[ns+"/"+name]
	if !ok {
		return nil, fmt.Errorf("no such package")
	}
	return out, nil
}

func (f *fakeCatalog) Ready() bool { return !f.notdone }

type fakeInstances struct {
	insts   []domain.Instance
	entries map[int][]string
	wrote   map[int][]string
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

func (f *fakeInstances) ReplaceMods(_ context.Context, num int, entries []string, _ ...string) (bool, error) {
	if f.wrote == nil {
		f.wrote = map[int][]string{}
	}
	f.wrote[num] = entries
	return true, nil
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

func TestRefreshOneFindsOnlyNewerPins(t *testing.T) {
	insts := &fakeInstances{entries: map[int][]string{
		2: {
			"denikson/BepInExPack_Valheim/5.4.2202",
			"Smoothbrain/Mining/1.3.4",
			"blacks7ar/BowPlugin/1.8.7",
			"Unindexed/Thing/1.0.0",
			"BareName",
		},
	}}
	cat := &fakeCatalog{latest: map[string]string{
		"denikson/BepInExPack_Valheim": "5.4.2202",
		"Smoothbrain/Mining":           "1.3.9",
		"blacks7ar/BowPlugin":          "1.8.7",
	}}

	ups, err := New(insts, cat).RefreshOne(context.Background(), 2)
	if err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}
	if len(ups) != 1 {
		t.Fatalf("want 1 update, got %d: %+v", len(ups), ups)
	}
	if ups[0].Ref.FullName() != "Smoothbrain-Mining" || ups[0].Current != "1.3.4" || ups[0].Latest != "1.3.9" {
		t.Errorf("unexpected update: %+v", ups[0])
	}
}

// An unpinned entry has no version to compare against, so it can never be
// reported as out of date: doing so would offer an update that is a no-op.
func TestUnpinnedEntriesAreNotUpdates(t *testing.T) {
	insts := &fakeInstances{entries: map[int][]string{1: {"Smoothbrain-Mining"}}}
	cat := &fakeCatalog{latest: map[string]string{"Smoothbrain/Mining": "1.3.9"}}

	ups, err := New(insts, cat).RefreshOne(context.Background(), 1)
	if err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}
	if len(ups) != 0 {
		t.Fatalf("want no updates for an unpinned entry, got %+v", ups)
	}
}

func TestRefreshOneWaitsForTheCatalog(t *testing.T) {
	insts := &fakeInstances{entries: map[int][]string{1: {"Smoothbrain/Mining/1.3.4"}}}
	cat := &fakeCatalog{notdone: true, latest: map[string]string{"Smoothbrain/Mining": "1.3.9"}}

	if _, err := New(insts, cat).RefreshOne(context.Background(), 1); err == nil {
		t.Fatal("want an error while the catalog is still indexing, got nil")
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
			"Smoothbrain/Mining":  "1.3.9",
			"blacks7ar/BowPlugin": "1.9.0",
		},
		tree: map[string][]string{
			"Smoothbrain/Mining": {"Smoothbrain/Mining/1.3.9"},
		},
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

	got := insts.wrote[1]
	want := []string{
		"denikson/BepInExPack_Valheim/5.4.2202",
		"Smoothbrain/Mining/1.3.9",
		"blacks7ar/BowPlugin/1.8.7",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("wrote %v, want %v", got, want)
	}
}

func TestApplyAddsAndBumpsDependencies(t *testing.T) {
	insts := &fakeInstances{entries: map[int][]string{
		1: {"Smoothbrain/Mining/1.3.4", "Shared/Lib/1.0.0"},
	}}
	cat := &fakeCatalog{
		latest: map[string]string{"Smoothbrain/Mining": "1.3.9"},
		tree: map[string][]string{
			"Smoothbrain/Mining": {
				"Smoothbrain/Mining/1.3.9",
				"Shared/Lib/2.0.0",
				"New/Dependency/0.1.0",
			},
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
		latest: map[string]string{"Smoothbrain/Mining": "1.3.9"},
		tree: map[string][]string{
			"Smoothbrain/Mining": {"Smoothbrain/Mining/1.3.9", "Shared/Lib/2.0.0"},
		},
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
	if cat.calls != 0 {
		t.Errorf("resolved %d trees for a selection with no updates, want 0", cat.calls)
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

func TestRefreshSkipsVanillaWorlds(t *testing.T) {
	insts := &fakeInstances{
		insts: []domain.Instance{
			{Number: 1, GameID: domain.GameValheim, Source: domain.SourceVanilla},
		},
		entries: map[int][]string{1: {"Smoothbrain/Mining/1.3.4"}},
	}
	cat := &fakeCatalog{latest: map[string]string{"Smoothbrain/Mining": "1.3.9"}}

	c := New(insts, cat)
	c.Refresh(context.Background())
	if got := c.Count(1); got != 0 {
		t.Errorf("a Vanilla world has no mods to update, got %d", got)
	}
}
