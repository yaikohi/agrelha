package mods

import (
	"context"
	"maps"
	"testing"

	"agrelha/internal/ports"
)

type memoryStateStore struct {
	doc ports.Document
}

func (m *memoryStateStore) Get(ctx context.Context, path string) (ports.Document, error) {
	return m.doc, nil
}

func (m *memoryStateStore) Put(ctx context.Context, path string, doc ports.Document, msg string) error {
	m.doc = doc
	return nil
}

func (m *memoryStateStore) Patch(ctx context.Context, path string, msg string, mutate func(doc *ports.Document) (bool, error)) (bool, error) {
	docCopy := ports.Document{
		Data:        make(map[string]string),
		Annotations: make(map[string]string),
	}
	maps.Copy(docCopy.Data, m.doc.Data)
	maps.Copy(docCopy.Annotations, m.doc.Annotations)
	changed, err := mutate(&docCopy)
	if err != nil || !changed {
		return false, err
	}
	m.doc = docCopy
	return true, nil
}

func (m *memoryStateStore) Delete(ctx context.Context, path string, msg string) error {
	m.doc = ports.Document{}
	return nil
}

func (m *memoryStateStore) PutTree(ctx context.Context, dirPath string, docs map[string]ports.Document, msg string) error {
	return nil
}

func TestModsOperations(t *testing.T) {
	ctx := context.Background()
	store := &memoryStateStore{
		doc: ports.Document{
			Data: map[string]string{
				"mods.txt": "denikson/BepInExPack_Valheim/5.4.2202\n# comments preserved\nvalheim/jotunn/2.20.0\n",
			},
		},
	}
	mgr := New(store, "manifests/valheim-mods.yaml")

	// 1. Parse
	parsed := Parse(store.doc.Data["mods.txt"])
	if len(parsed) != 2 || parsed[0] != "denikson/BepInExPack_Valheim/5.4.2202" || parsed[1] != "valheim/jotunn/2.20.0" {
		t.Fatalf("unexpected Parse: %+v", parsed)
	}

	// 2. Install existing -> no change
	changed, err := mgr.Install(ctx, []string{"valheim/jotunn/2.20.0"})
	if err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if changed {
		t.Fatalf("expected changed=false for already installed mod")
	}

	// 3. Install new mod
	changed, err = mgr.Install(ctx, []string{"author/newmod/1.0.0"})
	if err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true for new mod")
	}

	// 4. Remove
	changed, err = mgr.Remove(ctx, "valheim/jotunn")
	if err != nil {
		t.Fatalf("Remove error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true when removing existing mod")
	}
	for _, p := range Parse(store.doc.Data["mods.txt"]) {
		if p == "valheim/jotunn/2.20.0" {
			t.Fatalf("jotunn still present after Remove")
		}
	}

	// 5. Replace
	changed, err = mgr.Replace(ctx, []string{"mod/a/1.0", "mod/b/2.0"})
	if err != nil {
		t.Fatalf("Replace error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true on replace")
	}
	if store.doc.Data["mods.txt"] != "mod/a/1.0\nmod/b/2.0\n" {
		t.Fatalf("unexpected mods.txt after Replace: %q", store.doc.Data["mods.txt"])
	}

	// 6. Replace unchanged -> returns false
	changed, err = mgr.Replace(ctx, []string{"mod/b/2.0", "mod/a/1.0"})
	if err != nil || changed {
		t.Fatalf("expected changed=false when replacing with identical mods, got %v, err: %v", changed, err)
	}

	// Replace with empty strings and duplicate entries
	_, _ = mgr.Replace(ctx, []string{"", "mod/c/1.0", "mod/c/1.0"})

	// InstalledMods on non-nil doc.Data
	mods, err := mgr.InstalledMods(ctx)
	if err != nil || len(mods) != 1 || mods[0] != "mod/c/1.0" {
		t.Fatalf("unexpected InstalledMods: %v, err: %v", mods, err)
	}

	// 7. Remove nonexistent -> returns false
	changed, err = mgr.Remove(ctx, "nonexistent/mod")
	if err != nil || changed {
		t.Fatalf("expected changed=false when removing nonexistent mod, got %v, err: %v", changed, err)
	}
}

type failingStore struct {
	getErr error
}

func (f *failingStore) Get(context.Context, string) (ports.Document, error) {
	return ports.Document{}, f.getErr
}
func (f *failingStore) Put(context.Context, string, ports.Document, string) error { return nil }
func (f *failingStore) Delete(context.Context, string, string) error              { return nil }
func (f *failingStore) PutTree(context.Context, string, map[string]ports.Document, string) error {
	return nil
}
func (f *failingStore) Patch(ctx context.Context, path, msg string, fn func(*ports.Document) (bool, error)) (bool, error) {
	var doc ports.Document
	return fn(&doc)
}

func TestModsEdgeCasesAndErrors(t *testing.T) {
	ctx := context.Background()

	// 1. Nil Manager
	var nilMgr *Manager
	mods, err := nilMgr.InstalledMods(ctx)
	if err != nil || mods != nil {
		t.Errorf("expected nil mods from nil manager, got %v, err: %v", mods, err)
	}

	// 2. WithModReader
	readerMgr := New(nil, "", WithModReader(func(ctx context.Context) ([]string, error) {
		return []string{"mod1", "mod2"}, nil
	}))
	mods, err = readerMgr.InstalledMods(ctx)
	if err != nil || len(mods) != 2 {
		t.Errorf("unexpected InstalledMods with reader: %v, err: %v", mods, err)
	}

	// 3. Nil store returns ErrNotImplemented
	nilStoreMgr := New(nil, "")
	if _, err := nilStoreMgr.InstalledMods(ctx); err != ports.ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented for InstalledMods, got %v", err)
	}
	if _, err := nilStoreMgr.Install(ctx, []string{"m1"}); err != ports.ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented for Install, got %v", err)
	}
	if _, err := nilStoreMgr.Replace(ctx, []string{"m1"}); err != ports.ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented for Replace, got %v", err)
	}
	if _, err := nilStoreMgr.Remove(ctx, "m1"); err != ports.ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented for Remove, got %v", err)
	}

	// 4. Get error and nil doc.Data in InstalledMods
	getFailMgr := New(&failingStore{getErr: context.Canceled}, "path")
	if _, err := getFailMgr.InstalledMods(ctx); err != context.Canceled {
		t.Errorf("expected Get error, got %v", err)
	}

	nilDataMgr := New(&memoryStateStore{doc: ports.Document{Data: nil}}, "path")
	mods, err = nilDataMgr.InstalledMods(ctx)
	if err != nil || mods != nil {
		t.Errorf("expected nil mods on nil doc.Data, got %v, err: %v", mods, err)
	}

	// 5. Install & Replace on empty doc.Data
	nilDocStore := &failingStore{}
	emptyMgr := New(nilDocStore, "path")
	if _, err := emptyMgr.Install(ctx, []string{"author/mod/1.0"}); err != nil {
		t.Errorf("Install on nil doc failed: %v", err)
	}
	if _, err := emptyMgr.Replace(ctx, []string{"author/mod/1.0"}); err != nil {
		t.Errorf("Replace on nil doc failed: %v", err)
	}
}

