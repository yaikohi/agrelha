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
}
