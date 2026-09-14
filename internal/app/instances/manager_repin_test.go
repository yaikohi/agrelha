package instances

import (
	"context"
	"strings"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type repinStore struct {
	txt string
}

func (s *repinStore) Get(context.Context, string) (ports.Document, error) {
	return ports.Document{Data: map[string]string{"mods.txt": s.txt}}, nil
}
func (s *repinStore) Put(context.Context, string, ports.Document, string) error { return nil }
func (s *repinStore) Delete(context.Context, string, string) error              { return nil }
func (s *repinStore) PutTree(context.Context, string, map[string]ports.Document, string) error {
	return nil
}
func (s *repinStore) Patch(_ context.Context, _ string, _ string, mutate func(*ports.Document) (bool, error)) (bool, error) {
	doc := ports.Document{Data: map[string]string{"mods.txt": s.txt}}
	changed, err := mutate(&doc)
	if err != nil {
		return false, err
	}
	if changed {
		s.txt = doc.Data["mods.txt"]
	}
	return changed, nil
}

type fixedRepo struct{ inst domain.Instance }

func (r fixedRepo) Upsert(domain.Instance) error                { return nil }
func (r fixedRepo) Get(int) (*domain.Instance, error)           { i := r.inst; return &i, nil }
func (r fixedRepo) List() ([]domain.Instance, error)            { return []domain.Instance{r.inst}, nil }
func (r fixedRepo) UpdateState(int, domain.InstanceState) error { return nil }
func (r fixedRepo) Delete(int) error                            { return nil }

func repinManager(st *repinStore) *InstanceManager {
	m := NewInstanceManager(nil, st, nil, 0, 0, 0, "manifests/valheim", "", nil, "valheim",
		WithGameID(domain.GameValheim))
	return m
}

func lines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

// A slug typed with an explicit version pins that version even over an existing
// line: it is the manual way back to a build that worked, including downgrades.
func TestInstallModRepinsWhenTheSlugCarriesAVersion(t *testing.T) {
	st := &repinStore{txt: "denikson/BepInExPack_Valheim/5.4.2202\nSmoothbrain/Mining/1.3.9\n"}
	m := repinManager(st)
	m.repo = fixedRepo{inst: domain.Instance{Number: 1, GameID: domain.GameValheim, Source: domain.SourceModlist}}

	if _, err := m.InstallMod(context.Background(), 1, "Smoothbrain-Mining-1.3.4"); err != nil {
		t.Fatalf("InstallMod: %v", err)
	}

	want := []string{"denikson/BepInExPack_Valheim/5.4.2202", "Smoothbrain/Mining/1.3.4"}
	if got := lines(st.txt); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("mods.txt = %v, want %v (repinned in place, order preserved)", got, want)
	}
}

// Without a version the old behaviour stands: an installed mod is left alone
// rather than silently bumped to whatever is latest today.
func TestInstallModStillSkipsAnUnversionedSlugAlreadyPresent(t *testing.T) {
	st := &repinStore{txt: "Smoothbrain/Mining/1.3.4\n"}
	m := repinManager(st)
	m.repo = fixedRepo{inst: domain.Instance{Number: 1, GameID: domain.GameValheim, Source: domain.SourceModlist}}
	m.versionResolver = func(context.Context, string) (string, error) { return "1.3.9", nil }

	if _, err := m.InstallMod(context.Background(), 1, "Smoothbrain-Mining"); err != nil {
		t.Fatalf("InstallMod: %v", err)
	}
	if got := st.txt; got != "Smoothbrain/Mining/1.3.4\n" {
		t.Errorf("mods.txt = %q, want the existing pin untouched", got)
	}
}
