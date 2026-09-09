package local

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"agrelha/internal/ports"
	_ "modernc.org/sqlite"
)

func setupTestStore(t *testing.T) (*Adapter, *sql.DB) {
	t.Helper()
	dir := t.TempDir()

	db, err := sql.Open("sqlite", filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	adapter, err := New(filepath.Join(dir, "root"), WithDB(db))
	if err != nil {
		t.Fatalf("new local adapter: %v", err)
	}
	return adapter, db
}

func TestLocalStateStore_PutAndGet(t *testing.T) {
	ctx := context.Background()
	store, _ := setupTestStore(t)

	doc := ports.Document{
		Data: map[string]string{
			"server.properties": "motd=Hello World",
			"whitelist.txt":     "player1\nplayer2",
		},
		Annotations: map[string]string{
			"agrelha.dev/tier":   "medium",
			"agrelha.dev/loader": "neoforge",
		},
	}

	if err := store.Put(ctx, "instances/mc-01/config.yaml", doc, "initial commit"); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	got, err := store.Get(ctx, "instances/mc-01/config.yaml")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if got.Data["server.properties"] != "motd=Hello World" {
		t.Errorf("got motd %q, want 'motd=Hello World'", got.Data["server.properties"])
	}
	if got.Annotations["agrelha.dev/tier"] != "medium" {
		t.Errorf("got tier %q, want 'medium'", got.Annotations["agrelha.dev/tier"])
	}
}

func TestLocalStateStore_NoOpGuarantee(t *testing.T) {
	ctx := context.Background()
	store, _ := setupTestStore(t)

	doc := ports.Document{
		Data: map[string]string{"key": "value1"},
	}

	if err := store.Put(ctx, "cfg.yaml", doc, "commit 1"); err != nil {
		t.Fatalf("put 1 failed: %v", err)
	}

	revs1, err := store.History(ctx, "cfg.yaml")
	if err != nil || len(revs1) != 1 {
		t.Fatalf("expected 1 revision, got %d (err: %v)", len(revs1), err)
	}

	// Putting identical content must be a no-op: no new revision
	if err := store.Put(ctx, "cfg.yaml", doc, "commit 2 (noop)"); err != nil {
		t.Fatalf("put 2 failed: %v", err)
	}

	revs2, err := store.History(ctx, "cfg.yaml")
	if err != nil || len(revs2) != 1 {
		t.Fatalf("expected still 1 revision after noop write, got %d", len(revs2))
	}

	// Changing content creates revision 2
	doc.Data["key"] = "value2"
	if err := store.Put(ctx, "cfg.yaml", doc, "commit 3 (change)"); err != nil {
		t.Fatalf("put 3 failed: %v", err)
	}

	revs3, err := store.History(ctx, "cfg.yaml")
	if err != nil || len(revs3) != 2 {
		t.Fatalf("expected 2 revisions after changed write, got %d", len(revs3))
	}
}

func TestLocalStateStore_Patch(t *testing.T) {
	ctx := context.Background()
	store, _ := setupTestStore(t)

	doc := ports.Document{
		Data: map[string]string{"foo": "bar"},
	}
	_ = store.Put(ctx, "doc.yaml", doc, "init")

	// Patch with no change
	changed, err := store.Patch(ctx, "doc.yaml", "no change", func(d *ports.Document) (bool, error) {
		return false, nil
	})
	if err != nil || changed {
		t.Errorf("patch no-change: changed=%v, err=%v", changed, err)
	}

	// Patch with mutation
	changed, err = store.Patch(ctx, "doc.yaml", "add field", func(d *ports.Document) (bool, error) {
		d.Data["added"] = "true"
		return true, nil
	})
	if err != nil || !changed {
		t.Errorf("patch with mutation: changed=%v, err=%v", changed, err)
	}

	got, err := store.Get(ctx, "doc.yaml")
	if err != nil || got.Data["added"] != "true" {
		t.Errorf("got updated data: %+v, err: %v", got.Data, err)
	}
}

func TestLocalStateStore_Delete(t *testing.T) {
	ctx := context.Background()
	store, _ := setupTestStore(t)

	doc := ports.Document{Raw: []byte("simple content")}
	_ = store.Put(ctx, "sample.txt", doc, "init")

	if err := store.Delete(ctx, "sample.txt", "delete"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err := store.Get(ctx, "sample.txt")
	if !os.IsNotExist(err) {
		t.Errorf("expected IsNotExist, got %v", err)
	}

	// Deleting non-existent file is a no-op
	if err := store.Delete(ctx, "nonexistent.txt", "delete non-existent"); err != nil {
		t.Errorf("delete nonexistent returned err: %v", err)
	}
}

func TestLocalStateStore_PutTree(t *testing.T) {
	ctx := context.Background()
	store, _ := setupTestStore(t)

	tree := map[string]ports.Document{
		"file1.txt": {Raw: []byte("content1")},
		"file2.txt": {Raw: []byte("content2")},
	}

	if err := store.PutTree(ctx, "bundle", tree, "create bundle"); err != nil {
		t.Fatalf("PutTree failed: %v", err)
	}

	f1, err := store.Get(ctx, "bundle/file1.txt")
	if err != nil || !bytes.Equal(f1.Raw, []byte("content1")) {
		t.Errorf("get file1: %+v, err: %v", f1, err)
	}

	// Update tree: remove file2, add file3
	tree2 := map[string]ports.Document{
		"file1.txt": {Raw: []byte("content1")},
		"file3.txt": {Raw: []byte("content3")},
	}

	if err := store.PutTree(ctx, "bundle", tree2, "update bundle"); err != nil {
		t.Fatalf("PutTree 2 failed: %v", err)
	}

	_, err = store.Get(ctx, "bundle/file2.txt")
	if !os.IsNotExist(err) {
		t.Errorf("file2 should have been deleted, got err: %v", err)
	}
	f3, err := store.Get(ctx, "bundle/file3.txt")
	if err != nil || !bytes.Equal(f3.Raw, []byte("content3")) {
		t.Errorf("file3 missing or content mismatch: %v", err)
	}
}

func TestLocalStateStore_Rollback(t *testing.T) {
	ctx := context.Background()
	store, _ := setupTestStore(t)

	path := "cfg.yaml"
	_ = store.Put(ctx, path, ports.Document{Data: map[string]string{"v": "1"}}, "v1")
	_ = store.Put(ctx, path, ports.Document{Data: map[string]string{"v": "2"}}, "v2")

	history, err := store.History(ctx, path)
	if err != nil || len(history) != 2 {
		t.Fatalf("expected 2 revisions, got %d, err=%v", len(history), err)
	}

	// Roll back to revision 1
	if err := store.Rollback(ctx, path, history[0].ID, "rollback to v1"); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}

	doc, err := store.Get(ctx, path)
	if err != nil || doc.Data["v"] != "1" {
		t.Errorf("expected rolled back value '1', got %+v, err=%v", doc.Data, err)
	}
}

func TestLocalStateStore_PathEscaping(t *testing.T) {
	ctx := context.Background()
	store, _ := setupTestStore(t)

	badPaths := []string{
		"../escape",
		"../../etc/passwd",
		"/absolute/path",
	}

	for _, p := range badPaths {
		if err := store.Put(ctx, p, ports.Document{Raw: []byte("foo")}, "bad"); err == nil {
			t.Errorf("expected error for escaping path %q, got nil", p)
		}
	}
}
