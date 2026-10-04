package instances

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

// cfStore holds both halves of a Minecraft Mod list, as the real ConfigMap does.
type cfStore struct{ mods, curseforge string }

func (s *cfStore) doc() ports.Document {
	return ports.Document{Data: map[string]string{
		"mods.txt":       s.mods,
		"curseforge.txt": s.curseforge,
	}}
}

func (s *cfStore) Get(context.Context, string) (ports.Document, error)       { return s.doc(), nil }
func (s *cfStore) Put(context.Context, string, ports.Document, string) error { return nil }
func (s *cfStore) Delete(context.Context, string, string) error              { return nil }
func (s *cfStore) PutTree(context.Context, string, map[string]ports.Document, string) error {
	return nil
}
func (s *cfStore) Patch(_ context.Context, _ string, _ string, mutate func(*ports.Document) (bool, error)) (bool, error) {
	doc := s.doc()
	changed, err := mutate(&doc)
	if changed {
		s.mods = doc.Data["mods.txt"]
		s.curseforge = doc.Data["curseforge.txt"]
	}
	return changed, err
}

func cfManager(st *cfStore, resolve func(ctx context.Context, slug, mcVersion, loader string) ([]string, error)) *InstanceManager {
	return &InstanceManager{
		gameID:           domain.GameMinecraft,
		stateStore:       st,
		instancesRelPath: "manifests/minecraft-modded",
		cfResolver:       resolve,
		repo: &ipRepo{items: []domain.Instance{{
			GameID: domain.GameMinecraft, Number: 3, Name: "bob", Slug: "bob",
			Source: domain.SourceModlist, Loader: domain.LoaderFabric, MCVersion: "1.21.1",
		}}},
	}
}

// ADR 0004. The server detects a missing CurseForge dependency and fails; it
// cannot resolve one. So every dependency must be written into the list before
// the server ever starts, or the world crash-loops at boot.
func TestInstallCurseForgeWritesEveryDependencyDown(t *testing.T) {
	st := &cfStore{mods: "terralith:2.6.4\n"}
	mgr := cfManager(st, func(_ context.Context, slug, mcVersion, loader string) ([]string, error) {
		if slug != "jei" || mcVersion != "1.21.1" || loader != "fabric" {
			t.Errorf("resolver must receive the world's version and loader, got %s %s %s", slug, mcVersion, loader)
		}
		return []string{"jei:4593548", "architectury:111", "cloth-config:222"}, nil
	})

	added, err := mgr.InstallCurseForgeMod(context.Background(), 3, "jei", "tester")
	if err != nil {
		t.Fatalf("InstallCurseForgeMod: %v", err)
	}
	if added != 3 {
		t.Errorf("want 3 entries written (mod + 2 deps), got %d", added)
	}

	want := "jei:4593548\narchitectury:111\ncloth-config:222\n"
	if st.curseforge != want {
		t.Errorf("curseforge.txt = %q, want %q", st.curseforge, want)
	}
	if st.mods != "terralith:2.6.4\n" {
		t.Errorf("the Modrinth list must be untouched, got %q", st.mods)
	}
}

// Every entry is pinned. A bare slug lets CurseForge pick the newest file at
// boot, which makes the running mod set unknowable and an export a guess.
func TestInstalledCurseForgeEntriesArePinned(t *testing.T) {
	st := &cfStore{}
	mgr := cfManager(st, func(context.Context, string, string, string) ([]string, error) {
		return []string{"jei:4593548"}, nil
	})
	if _, err := mgr.InstallCurseForgeMod(context.Background(), 3, "jei"); err != nil {
		t.Fatalf("InstallCurseForgeMod: %v", err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(st.curseforge), "\n") {
		if !strings.Contains(line, ":") {
			t.Errorf("entry %q is unpinned", line)
		}
	}
}

// Nothing is written when resolution fails. A half-applied list would download
// what it could and fail on the rest at boot - the worst of both.
func TestInstallCurseForgeWritesNothingWhenResolutionFails(t *testing.T) {
	st := &cfStore{curseforge: "existing:1\n"}
	mgr := cfManager(st, func(context.Context, string, string, string) ([]string, error) {
		return nil, errors.New("this mod's author does not allow third-party downloads")
	})

	if _, err := mgr.InstallCurseForgeMod(context.Background(), 3, "restricted"); err == nil {
		t.Fatal("want an error when resolution fails")
	}
	if st.curseforge != "existing:1\n" {
		t.Errorf("the list must be untouched, got %q", st.curseforge)
	}
}

// Without a configured CurseForge client, an install is refused rather than
// written unresolved - an unresolved entry is a boot-time crash loop.
func TestInstallCurseForgeRefusedWhenUnconfigured(t *testing.T) {
	st := &cfStore{}
	mgr := cfManager(st, nil)
	_, err := mgr.InstallCurseForgeMod(context.Background(), 3, "jei")
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("want a 'not configured' refusal, got %v", err)
	}
	if st.curseforge != "" {
		t.Error("nothing may be written when CurseForge is unavailable")
	}
}

// Re-installing the same mod at a different file re-pins rather than duplicating.
func TestInstallCurseForgeRepinsRatherThanDuplicating(t *testing.T) {
	st := &cfStore{curseforge: "jei:1111\n"}
	mgr := cfManager(st, func(context.Context, string, string, string) ([]string, error) {
		return []string{"jei:4593548"}, nil
	})
	if _, err := mgr.InstallCurseForgeMod(context.Background(), 3, "jei"); err != nil {
		t.Fatalf("InstallCurseForgeMod: %v", err)
	}
	if st.curseforge != "jei:4593548\n" {
		t.Errorf("want the pin replaced, got %q", st.curseforge)
	}
}

func TestRemoveCurseForgeLeavesTheModrinthListAlone(t *testing.T) {
	st := &cfStore{mods: "terralith:2.6.4\n", curseforge: "jei:4593548\narchitectury:111\n"}
	mgr := cfManager(st, nil)

	if err := mgr.RemoveCurseForgeMod(context.Background(), 3, "jei", "tester"); err != nil {
		t.Fatalf("RemoveCurseForgeMod: %v", err)
	}
	if st.curseforge != "architectury:111\n" {
		t.Errorf("curseforge.txt = %q, want only the dependency left", st.curseforge)
	}
	if st.mods != "terralith:2.6.4\n" {
		t.Errorf("the Modrinth list must be untouched, got %q", st.mods)
	}
}

// The same slug on both catalogues is two different mods. Removing one must not
// touch the other.
func TestRemoveIsScopedToOneProvider(t *testing.T) {
	st := &cfStore{mods: "jei:1.2.3\n", curseforge: "jei:4593548\n"}
	mgr := cfManager(st, nil)

	if err := mgr.RemoveCurseForgeMod(context.Background(), 3, "jei"); err != nil {
		t.Fatalf("RemoveCurseForgeMod: %v", err)
	}
	if st.curseforge != "" {
		t.Errorf("want the CurseForge entry gone, got %q", st.curseforge)
	}
	if st.mods != "jei:1.2.3\n" {
		t.Errorf("the Modrinth mod of the same name must survive, got %q", st.mods)
	}
}

func TestGetModListReadsBothHalves(t *testing.T) {
	st := &cfStore{mods: "terralith:2.6.4\n", curseforge: "jei:4593548\n"}
	mgr := cfManager(st, nil)

	got, err := mgr.GetModList(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetModList: %v", err)
	}
	if got.Primary != "terralith:2.6.4\n" || got.CurseForge != "jei:4593548\n" {
		t.Errorf("GetModList = %+v, want both halves", got)
	}
	if !got.HasCurseForge() {
		t.Error("want HasCurseForge so the renderer wires CURSEFORGE_FILES")
	}
}

// A Valheim Instance has one catalogue; asking for its CurseForge half must not
// invent one.
func TestGetModListLeavesCurseForgeEmptyForValheim(t *testing.T) {
	st := &cfStore{mods: "Smoothbrain/Mining/1.1.6\n", curseforge: "should-be-ignored:1\n"}
	mgr := cfManager(st, nil)
	mgr.gameID = domain.GameValheim

	got, err := mgr.GetModList(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetModList: %v", err)
	}
	if got.CurseForge != "" {
		t.Errorf("Valheim has no CurseForge list, got %q", got.CurseForge)
	}
}
