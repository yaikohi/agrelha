package instances

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"agrelha/internal/ports"
)

func TestAMissingPathIsNotExistNotAnEmptyDocument(t *testing.T) {
	m := newMockStateStore()
	_, err := m.Get(context.Background(), "manifests/valheim/instance-01/mods.yaml")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf(`reading a path that does not exist returned %v.

Real git returns fs.ErrNotExist. A mock that answers "here is an empty document, no error" makes
every caller's absent-file branch unreachable, which is how a fallback that silently served stale
data passed its tests for weeks.`, err)
	}
}

func TestAPatchIsVisibleToTheNextRead(t *testing.T) {
	m := newMockStateStore()
	ctx := context.Background()
	path := "manifests/valheim/instance-01/mods.yaml"

	if _, err := m.Patch(ctx, path, "seed", func(doc *ports.Document) (bool, error) {
		doc.Data["mods.txt"] = "jei\n"
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	got, err := m.Get(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Data["mods.txt"] != "jei\n" {
		t.Errorf(`a patched value did not read back: %q.

Patch stored Document.Data while PutTree stored Document.Raw and neither read the other, so writes
vanished between calls. A test could install a mod and then assert it was gone.`, got.Data["mods.txt"])
	}
}

func TestADocumentWrittenAsBytesReadsBackWithItsKeys(t *testing.T) {
	m := newMockStateStore()
	ctx := context.Background()
	raw := []byte("apiVersion: v1\nkind: ConfigMap\ndata:\n  mods.txt: |\n    sodium\n")

	if err := m.PutTree(ctx, "compose/valheim/instance-01", map[string]ports.Document{
		"mods.yaml": {Raw: raw},
	}, "render"); err != nil {
		t.Fatal(err)
	}

	got, err := m.Get(ctx, "compose/valheim/instance-01/mods.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got.Data["mods.txt"] == "" {
		t.Error("a rendered file read back with no data map, so no test could see what a renderer actually wrote")
	}
}

func TestPutTreeRemovesFilesItWasNotGiven(t *testing.T) {
	m := newMockStateStore()
	ctx := context.Background()
	dir := "compose/valheim/instance-01"

	m.seed(dir+"/configs.yaml", map[string]string{"bepinex.cfg": "x"})
	m.seed(dir+"/mods.yaml", map[string]string{"mods.txt": "jei\n"})

	if err := m.PutTree(ctx, dir, map[string]ports.Document{
		"mods.yaml": {Data: map[string]string{"mods.txt": "jei\n"}},
	}, "render"); err != nil {
		t.Fatal(err)
	}

	if m.has(dir + "/configs.yaml") {
		t.Error(`PutTree left behind a file it was not given.

The real contract replaces the directory, which is exactly how a renderer that stopped emitting
configs.yaml deleted a world's config overrides in production. A mock that keeps the file makes
that class of bug invisible.`)
	}
}

func TestPutTreeLeavesOtherDirectoriesAlone(t *testing.T) {
	m := newMockStateStore()
	ctx := context.Background()
	m.seed("compose/valheim/instance-02/mods.yaml", map[string]string{"mods.txt": "other\n"})

	if err := m.PutTree(ctx, "compose/valheim/instance-01", map[string]ports.Document{
		"mods.yaml": {Data: map[string]string{"mods.txt": "jei\n"}},
	}, "render"); err != nil {
		t.Fatal(err)
	}
	if !m.has("compose/valheim/instance-02/mods.yaml") {
		t.Error("rendering one world deleted another world's state")
	}
}
