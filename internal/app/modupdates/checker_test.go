package modupdates

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

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
	insts                     []domain.Instance
	entries                   map[int][]string
	wrote                     map[int][]string
	details                   map[int]string
	err                       error
	listErr                   error
	replaceErr                error
	getInstErr                error
	installedErr              error
	failInstalledAfterReplace bool
}

func (f *fakeInstances) GetInstance(_ context.Context, num int) (*domain.Instance, error) {
	if f.getInstErr != nil {
		return nil, f.getInstErr
	}
	for _, inst := range f.insts {
		if inst.Number == num {
			return &inst, nil
		}
	}
	return nil, nil
}

func (f *fakeInstances) ListInstances(context.Context) ([]domain.Instance, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.insts, f.err
}

func (f *fakeInstances) GetInstalledMods(_ context.Context, num int) ([]string, error) {
	if f.installedErr != nil {
		return nil, f.installedErr
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.entries[num], nil
}

func (f *fakeInstances) ReplaceMods(_ context.Context, num int, entries []string, detail string, _ ...string) (bool, error) {
	if f.replaceErr != nil {
		return false, f.replaceErr
	}
	if f.wrote == nil {
		f.wrote = map[int][]string{}
		f.details = map[int]string{}
	}
	f.wrote[num] = entries
	f.details[num] = detail
	if f.failInstalledAfterReplace {
		f.installedErr = fmt.Errorf("fail after replace")
	}
	return true, nil
}

type plainInstances struct {
	insts   []domain.Instance
	entries map[int][]string
	listErr error
}

func (p *plainInstances) ListInstances(context.Context) ([]domain.Instance, error) {
	if p.listErr != nil {
		return nil, p.listErr
	}
	return p.insts, nil
}

func (p *plainInstances) GetInstalledMods(_ context.Context, num int) ([]string, error) {
	return p.entries[num], nil
}

func (p *plainInstances) ReplaceMods(context.Context, int, []string, string, ...string) (bool, error) {
	return true, nil
}

type fakeRestore struct {
	points   map[int]*domain.ModRestorePoint
	saveErr  error
	clearErr error
	getErr   error
}

func (f *fakeRestore) SaveRestorePoint(_ domain.GameID, number int, previous, applied []string) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	if f.points == nil {
		f.points = map[int]*domain.ModRestorePoint{}
	}
	f.points[number] = &domain.ModRestorePoint{Previous: previous, Applied: applied}
	return nil
}

func (f *fakeRestore) RestorePoint(_ domain.GameID, number int) (*domain.ModRestorePoint, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.points[number], nil
}

func (f *fakeRestore) ClearRestorePoint(_ domain.GameID, number int) error {
	if f.clearErr != nil {
		return f.clearErr
	}
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

type fakeInstanceCatalog struct {
	fakeCatalog
	instanceVersions map[string]string
	instanceTrees    map[string][]string
}

func (f *fakeInstanceCatalog) LatestVersionForInstance(_ context.Context, ref domain.ModRef, inst domain.Instance) (string, []string, error) {
	key := fmt.Sprintf("%s|%s|%s", ref.Name, inst.MCVersion, inst.Loader)
	v, ok := f.instanceVersions[key]
	if !ok {
		return "", nil, nil
	}
	return v, nil, nil
}

func (f *fakeInstanceCatalog) ResolveTreeForInstance(_ context.Context, ref domain.ModRef, inst domain.Instance) ([]string, error) {
	key := fmt.Sprintf("%s|%s|%s", ref.Name, inst.MCVersion, inst.Loader)
	return f.instanceTrees[key], nil
}

func TestRefreshFiltersByInstanceLoader(t *testing.T) {
	insts := &fakeInstances{
		insts: []domain.Instance{
			{Number: 1, Name: "FabricWorld", GameID: domain.GameMinecraft, Loader: domain.LoaderFabric, MCVersion: "1.21.1", Source: domain.SourceModlist},
			{Number: 2, Name: "NeoWorld", GameID: domain.GameMinecraft, Loader: domain.LoaderNeoForge, MCVersion: "1.21.1", Source: domain.SourceModlist},
		},
		entries: map[int][]string{
			1: {"sodium:0.5.11+mc1.21"},
			2: {"sodium:0.5.11+mc1.21"},
		},
	}
	cat := &fakeInstanceCatalog{
		instanceVersions: map[string]string{
			"sodium|1.21.1|fabric":   "0.5.11+mc1.21", // no newer version for fabric
			"sodium|1.21.1|neoforge": "0.6.0+mc1.21.1", // newer version available for neoforge
		},
	}

	c := New(insts, cat, WithGameID(domain.GameMinecraft))

	repFabric, err := c.RefreshOne(context.Background(), 1)
	if err != nil {
		t.Fatalf("RefreshOne(1): %v", err)
	}
	if len(repFabric.Updates) != 0 {
		t.Errorf("Fabric world must NOT show updates when only NeoForge has one: got %+v", repFabric.Updates)
	}

	repNeo, err := c.RefreshOne(context.Background(), 2)
	if err != nil {
		t.Fatalf("RefreshOne(2): %v", err)
	}
	if len(repNeo.Updates) != 1 {
		t.Fatalf("NeoForge world must show 1 update, got %d", len(repNeo.Updates))
	}
	u := repNeo.Updates[0]
	if u.Ref.Name != "sodium" || u.Current != "0.5.11+mc1.21" || u.Latest != "0.6.0+mc1.21.1" {
		t.Errorf("unexpected update: %+v", u)
	}
}

func TestMinecraftModUpdatesApplyAndUndo(t *testing.T) {
	insts := &fakeInstances{
		insts: []domain.Instance{
			{Number: 2, Name: "NeoWorld", GameID: domain.GameMinecraft, Loader: domain.LoaderNeoForge, MCVersion: "1.21.1", Source: domain.SourceModlist},
		},
		entries: map[int][]string{
			2: {"sodium:0.5.11+mc1.21", "cloth-config:15.0.127"},
		},
	}
	cat := &fakeInstanceCatalog{
		instanceVersions: map[string]string{
			"sodium|1.21.1|neoforge":       "0.6.0+mc1.21.1",
			"cloth-config|1.21.1|neoforge": "15.0.127",
		},
	}
	rs := &fakeRestore{}

	ctx := context.Background()
	c := New(insts, cat, WithGameID(domain.GameMinecraft), WithRestorePoints(rs))

	applied, err := c.Apply(ctx, 2, []string{"sodium"}, "tester")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("applied = %d, want 1", len(applied))
	}

	wantWrote := []string{"sodium:0.6.0+mc1.21.1", "cloth-config:15.0.127"}
	if strings.Join(insts.wrote[2], ",") != strings.Join(wantWrote, ",") {
		t.Errorf("wrote = %v, want %v", insts.wrote[2], wantWrote)
	}

	// Now undo
	insts.entries[2] = insts.wrote[2]
	rp, err := c.Undo(ctx, 2, "tester")
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	wantReverted := []string{"sodium:0.5.11+mc1.21", "cloth-config:15.0.127"}
	if strings.Join(rp.Previous, ",") != strings.Join(wantReverted, ",") {
		t.Errorf("reverted = %v, want %v", rp.Previous, wantReverted)
	}
}

func TestMinecraftUnpinnedModCanBeUpdatedAndPinned(t *testing.T) {
	insts := &fakeInstances{
		insts: []domain.Instance{
			{Number: 3, Name: "BobWorld", GameID: domain.GameMinecraft, Loader: domain.LoaderFabric, MCVersion: "1.21.1", Source: domain.SourceModlist},
		},
		entries: map[int][]string{
			3: {"xaeros-minimap"},
		},
	}
	cat := &fakeInstanceCatalog{
		instanceVersions: map[string]string{
			"xaeros-minimap|1.21.1|fabric": "24.7.1",
		},
	}

	ctx := context.Background()
	c := New(insts, cat, WithGameID(domain.GameMinecraft))

	rep, err := c.RefreshOne(ctx, 3)
	if err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}
	if len(rep.Updates) != 1 {
		t.Fatalf("want 1 update for unpinned Minecraft mod, got %d", len(rep.Updates))
	}
	if rep.Updates[0].Current != "(unpinned)" || rep.Updates[0].Latest != "24.7.1" {
		t.Errorf("unexpected update: %+v", rep.Updates[0])
	}

	applied, err := c.Apply(ctx, 3, nil, "tester")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("applied = %d, want 1", len(applied))
	}
	wantWrote := []string{"xaeros-minimap:24.7.1"}
	if strings.Join(insts.wrote[3], ",") != strings.Join(wantWrote, ",") {
		t.Errorf("wrote = %v, want %v", insts.wrote[3], wantWrote)
	}
}

func TestCheckerMethodsAndEdges(t *testing.T) {
	insts := &fakeInstances{
		insts: []domain.Instance{
			{Number: 1, Name: "VanillaWorld", GameID: domain.GameValheim, Source: domain.SourceVanilla},
			{Number: 2, Name: "PackWorld", GameID: domain.GameValheim, Source: domain.SourceModpack, Pack: &domain.Pack{Name: "Pack"}},
			{Number: 3, Name: "ModdedWorld", GameID: domain.GameValheim, Source: domain.SourceModlist},
		},
		entries: map[int][]string{
			3: {"denikson-BepInExPack_Valheim-5.4.2100"},
		},
	}
	cat := &fakeCatalog{
		latest: map[string]string{
			"denikson/BepInExPack_Valheim": "5.4.2202",
		},
	}

	// 1. WithInterval and Start
	c := New(insts, cat, WithInterval(10*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	c.Start(ctx)
	time.Sleep(25 * time.Millisecond)
	cancel()

	// 2. Nil checker calls
	var nilChecker *Checker
	nilChecker.Start(ctx)
	nilChecker.Refresh(ctx)
	if nilChecker.Counts() != nil {
		t.Errorf("expected nil Counts for nil checker")
	}
	if nilChecker.Total() != 0 {
		t.Errorf("expected 0 Total for nil checker")
	}
	if nilChecker.LastError(1) != nil {
		t.Errorf("expected nil LastError for nil checker")
	}
	if nilChecker.Pending(ctx, 1) {
		t.Errorf("expected false Pending for nil checker")
	}

	// 3. Counts and Total
	counts := c.Counts()
	if counts[3] != 1 {
		t.Errorf("counts[3] = %d, want 1", counts[3])
	}
	if tot := c.Total(); tot != 1 {
		t.Errorf("Total = %d, want 1", tot)
	}

	// 4. LastError
	if err := c.LastError(3); err != nil {
		t.Errorf("unexpected error on #3: %v", err)
	}

	// 5. Pending with expired TTL
	c.markPending(3, []string{"denikson/BepInExPack_Valheim"})
	c.mu.Lock()
	p := c.pend[3]
	p.at = time.Now().Add(-10 * time.Minute)
	c.pend[3] = p
	c.mu.Unlock()
	if c.Pending(ctx, 3) {
		t.Errorf("expected Pending to be false for expired pending")
	}

	// 6. Pending when installed mods match want
	c.markPending(3, []string{"denikson-BepInExPack_Valheim-5.4.2100"})
	if c.Pending(ctx, 3) {
		t.Errorf("expected Pending to be false when installed mod satisfies want")
	}

	// 7. Undo without restore points
	if _, err := c.Undo(ctx, 3, "tester"); err == nil {
		t.Errorf("expected error from Undo when restore is nil")
	}
	if rp, _ := c.RestoreAvailable(ctx, 3); rp != nil {
		t.Errorf("expected nil restore point when restore is nil")
	}

	// 8. Refresh error when ListInstances fails
	errInsts := &fakeInstances{listErr: fmt.Errorf("storage error")}
	cErr := New(errInsts, cat)
	cErr.Refresh(ctx)
}

func TestCheckerAllEdgeCases(t *testing.T) {
	ctx := context.Background()

	// 1. Refresh with failing RefreshOne
	failingRefreshInsts := &fakeInstances{
		insts: []domain.Instance{{Number: 1, Name: "broken", Source: domain.SourceModlist}},
		err:   fmt.Errorf("read error"),
	}
	cat := &fakeCatalog{}
	cRefreshFail := New(failingRefreshInsts, cat)
	cRefreshFail.Refresh(ctx)

	// 2. RefreshOne / Apply guards on nil receiver or components
	var nilChecker *Checker
	if _, err := nilChecker.RefreshOne(ctx, 1); err != nil {
		t.Errorf("expected nil error on nil checker RefreshOne")
	}
	if _, err := nilChecker.Apply(ctx, 1, nil, "actor"); err == nil {
		t.Errorf("expected error on nil checker Apply")
	}

	cNilCat := New(failingRefreshInsts, nil)
	if _, err := cNilCat.Apply(ctx, 1, nil, "actor"); err == nil {
		t.Errorf("expected error on Apply with nil catalog")
	}

	// 3. compute on Vanilla and PackDefined instances
	vanillaInsts := &fakeInstances{
		insts: []domain.Instance{
			{Number: 1, Source: domain.SourceVanilla},
			{Number: 2, Source: domain.SourceModpack, Pack: &domain.Pack{Name: "pack"}},
			{Number: 3, Source: domain.SourceModlist}, // empty mods
		},
		entries: map[int][]string{
			3: {"bareword"},
		},
	}
	cVanilla := New(vanillaInsts, cat, WithGameID(domain.GameValheim))
	rep1, _ := cVanilla.RefreshOne(ctx, 1)
	if rep1.Total != 0 {
		t.Errorf("expected 0 mods on vanilla")
	}
	rep2, _ := cVanilla.RefreshOne(ctx, 2)
	if rep2.Total != 0 {
		t.Errorf("expected 0 mods on modpack")
	}
	rep3, _ := cVanilla.RefreshOne(ctx, 3)
	if rep3.Total != 0 {
		t.Errorf("expected 0 valid mods on invalid entries")
	}

	// 4. Pending when GetInstalledMods fails
	cPendingFail := New(failingRefreshInsts, cat)
	cPendingFail.markPending(1, []string{"mod1"})
	if !cPendingFail.Pending(ctx, 1) {
		t.Errorf("expected Pending=true when GetInstalledMods fails")
	}

	// 5. Apply error branches:
	// 5a. available empty and RefreshOne fails
	cApplyFail1 := New(failingRefreshInsts, cat)
	if _, err := cApplyFail1.Apply(ctx, 1, nil, "actor"); err == nil {
		t.Errorf("expected error when available empty and RefreshOne fails")
	}

	// 5b. available empty after RefreshOne succeeds
	emptyInsts := &fakeInstances{
		insts: []domain.Instance{{Number: 1, Source: domain.SourceVanilla}},
	}
	cApplyEmpty := New(emptyInsts, cat)
	if targets, err := cApplyEmpty.Apply(ctx, 1, nil, "actor"); err != nil || targets != nil {
		t.Errorf("expected nil targets and nil err when available is empty")
	}

	// 5c. ReplaceMods fails in Apply
	goodCat := &fakeCatalog{
		latest: map[string]string{"ns/mod": "2.0.0"},
		tree:   map[string][]string{"ns/mod": {"ns/mod/2.0.0", "invalid/tree/entry", "ns/nover"}},
		down:   map[string]bool{},
	}
	failReplaceInsts := &fakeInstances{
		insts:      []domain.Instance{{Number: 1, Source: domain.SourceModlist}},
		entries:    map[int][]string{1: {"ns/mod/1.0.0", "invalid-entry"}},
		replaceErr: fmt.Errorf("git write failed"),
	}
	restore := &fakeRestore{
		saveErr:  fmt.Errorf("save error"),
		clearErr: fmt.Errorf("clear error"),
	}
	cReplaceFail := New(failReplaceInsts, goodCat, WithRestorePoints(restore))
	cReplaceFail.snap[1] = snapshot{report: Report{Updates: []domain.ModUpdate{{
		Ref: domain.ModRef{Namespace: "ns", Name: "mod", Version: "1.0.0"}, Latest: "2.0.0",
	}}}}
	if _, err := cReplaceFail.Apply(ctx, 1, nil, "actor"); err == nil {
		t.Errorf("expected error when ReplaceMods fails in Apply")
	}

	// 5d. resolveTree error in Apply
	cTreeFail := New(failReplaceInsts, cat)
	cTreeFail.snap[1] = snapshot{report: Report{Updates: []domain.ModUpdate{{
		Ref: domain.ModRef{Namespace: "ns", Name: "mod", Version: "1.0.0"}, Latest: "2.0.0",
	}}}}
	if _, err := cTreeFail.Apply(ctx, 1, nil, "actor"); err == nil {
		t.Errorf("expected error when resolveTree fails in Apply")
	}

	// 6. Undo error branches
	goodInsts := &fakeInstances{
		insts:   []domain.Instance{{Number: 1, Source: domain.SourceModlist}},
		entries: map[int][]string{1: {"ns/mod/2.0.0"}},
	}
	restoreGood := &fakeRestore{
		points: map[int]*domain.ModRestorePoint{
			1: {Previous: []string{"ns/mod/1.0.0"}, Applied: []string{"ns/mod/2.0.0"}},
		},
	}
	cUndoFail := New(goodInsts, goodCat, WithRestorePoints(restoreGood))
	// ReplaceMods fails in Undo
	goodInsts.replaceErr = fmt.Errorf("replace fail")
	if _, err := cUndoFail.Undo(ctx, 1, "actor"); err == nil {
		t.Errorf("expected error when ReplaceMods fails in Undo")
	}
	goodInsts.replaceErr = nil

	// Undo succeeds with clearErr and RefreshOne warning
	restoreGood.clearErr = fmt.Errorf("clear error")
	goodCat.down["ns/mod"] = true
	if rp, err := cUndoFail.Undo(ctx, 1, "actor"); err != nil || rp == nil {
		t.Errorf("expected Undo to succeed even with warning logs: %v", err)
	}
	goodCat.down["ns/mod"] = false

	// 7. RestoreAvailable edge cases:
	// GetInstalledMods error
	goodInsts.err = fmt.Errorf("read err")
	restoreGood.points[1] = &domain.ModRestorePoint{Applied: []string{"ns/mod/2.0.0"}}
	cRestoreFail := New(goodInsts, goodCat, WithRestorePoints(restoreGood))
	if _, err := cRestoreFail.RestoreAvailable(ctx, 1); err == nil {
		t.Errorf("expected error from RestoreAvailable when GetInstalledMods fails")
	}

	// !rp.Matches and ClearRestorePoint fails
	goodInsts.err = nil
	goodInsts.entries[1] = []string{"completely/different/1.0.0"}
	restoreGood.clearErr = fmt.Errorf("clear err")
	rp, err := cRestoreFail.RestoreAvailable(ctx, 1)
	if err != nil || rp != nil {
		t.Errorf("expected nil rp when entries do not match: %v, err: %v", rp, err)
	}

	// saveRestorePoint error logging
	cSaveFail := New(goodInsts, goodCat, WithRestorePoints(restore))
	cSaveFail.saveRestorePoint(1, []string{"old"}, []string{"new"})

	// 8. Start with ticker and nil guards
	nilChecker.Start(ctx)
	cNilCat.Start(ctx)

	cStart := New(goodInsts, goodCat, WithInterval(5*time.Millisecond))
	startCtx, cancelStart := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelStart()
	cStart.Start(startCtx)
	<-startCtx.Done()

	// 9. Refresh logs warning when RefreshOne fails on an instance
	failOneInsts := &fakeInstances{
		insts:        []domain.Instance{{Number: 10, Source: domain.SourceModlist}},
		installedErr: fmt.Errorf("read fail"),
	}
	cRefreshFailOne := New(failOneInsts, cat)
	cRefreshFailOne.Refresh(ctx)

	// 10. getInstance fallback without GetInstance interface
	plain := &plainInstances{
		insts:   []domain.Instance{{Number: 1, Source: domain.SourceModlist}},
		entries: map[int][]string{1: {"ns/mod/1.0.0"}},
	}
	cPlain := New(plain, goodCat)
	_ = cPlain.getInstance(ctx, 1) // found
	_ = cPlain.getInstance(ctx, 2) // not found
	plain.listErr = fmt.Errorf("list fail")
	_ = cPlain.getInstance(ctx, 1) // list err

	// 11. cat == nil in latestVersionForRef & resolveTreeForRef
	cNoCat := New(goodInsts, nil)
	_, _, _ = cNoCat.latestVersionForRef(ctx, domain.ModRef{Namespace: "ns", Name: "mod"}, domain.Instance{})
	_, _ = cNoCat.resolveTreeForRef(ctx, domain.ModRef{Namespace: "ns", Name: "mod"}, domain.Instance{})

	// 11b. ParseModRef failure in compute and Apply
	badRefInsts := &fakeInstances{
		insts:   []domain.Instance{{Number: 1, Source: domain.SourceModlist}},
		entries: map[int][]string{1: {"# comment", "valid/mod/1.0.0"}},
	}
	cBadRef := New(badRefInsts, goodCat)
	_, _ = cBadRef.RefreshOne(ctx, 1)
	cBadRef.snap[1] = snapshot{report: Report{Updates: []domain.ModUpdate{{
		Ref: domain.ModRef{Namespace: "valid", Name: "mod", Version: "1.0.0"}, Latest: "2.0.0",
	}}}}
	_, _ = cBadRef.Apply(ctx, 1, nil, "actor")

	// 12. Apply with GetInstalledMods error
	failPreviousInsts := &fakeInstances{
		insts:   []domain.Instance{{Number: 1, Source: domain.SourceModlist}},
		entries: map[int][]string{1: {"valid/mod/1.0.0"}},
	}
	cPreviousFail := New(failPreviousInsts, goodCat)
	cPreviousFail.snap[1] = snapshot{report: Report{Updates: []domain.ModUpdate{{
		Ref: domain.ModRef{Namespace: "valid", Name: "mod", Version: "1.0.0"}, Latest: "2.0.0",
	}}}}
	failPreviousInsts.err = fmt.Errorf("fail previous")
	if _, err := cPreviousFail.Apply(ctx, 1, nil, "actor"); err == nil {
		t.Errorf("expected error in Apply when GetInstalledMods fails")
	}

	// 13. Multiple missing and unreachable mods to test sorting
	multiErrCat := &fakeCatalog{
		down:   map[string]bool{"ns/unreach1": true, "ns/unreach2": true},
		latest: map[string]string{"ns/empty": ""},
	}
	multiErrInsts := &fakeInstances{
		insts: []domain.Instance{{Number: 1, Source: domain.SourceModlist}},
		entries: map[int][]string{1: {
			"ns/miss2/1.0.0", "ns/miss1/1.0.0",
			"ns/unreach2/1.0.0", "ns/unreach1/1.0.0",
			"ns/empty/1.0.0",
		}},
	}
	cMulti := New(multiErrInsts, multiErrCat)
	_, _ = cMulti.RefreshOne(ctx, 1)

	// 14. Report on nil checker and Pending on unknown instance
	if rep := nilChecker.Report(1); rep.Total != 0 {
		t.Errorf("expected empty report on nil checker")
	}
	if cMulti.Pending(ctx, 999) {
		t.Errorf("expected false pending for unknown number")
	}

	// 15. Undo with RestoreAvailable error
	restoreErr := &fakeRestore{getErr: fmt.Errorf("restore error")}
	cUndoErr := New(goodInsts, goodCat, WithRestorePoints(restoreErr))
	if _, err := cUndoErr.Undo(ctx, 1, "actor"); err == nil {
		t.Errorf("expected error when RestoreAvailable fails in Undo")
	}

	// 16. Undo when RefreshOne fails after successful replace
	undoFailInsts := &fakeInstances{
		entries:                   map[int][]string{1: {"valid/mod/2.0.0"}},
		failInstalledAfterReplace: true,
	}
	restoreUndo := &fakeRestore{
		points: map[int]*domain.ModRestorePoint{
			1: {Previous: []string{"valid/mod/1.0.0"}, Applied: []string{"valid/mod/2.0.0"}},
		},
	}
	cUndoRefreshFail := New(undoFailInsts, goodCat, WithRestorePoints(restoreUndo))
	if _, err := cUndoRefreshFail.Undo(ctx, 1, "actor"); err != nil {
		t.Errorf("expected Undo to succeed even if RefreshOne fails: %v", err)
	}
}

