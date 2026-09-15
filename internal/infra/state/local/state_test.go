package local

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
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
		if _, err := store.Get(ctx, p); err == nil {
			t.Errorf("expected error for Get escaping path %q, got nil", p)
		}
		if err := store.Delete(ctx, p, "del"); err == nil {
			t.Errorf("expected error for Delete escaping path %q, got nil", p)
		}
		if err := store.PutTree(ctx, p, nil, "tree"); err == nil {
			t.Errorf("expected error for PutTree escaping path %q, got nil", p)
		}
	}
}

func TestLocalStateStore_WithoutDB_And_RollbackEdgeCases(t *testing.T) {
	ctx := context.Background()
	noDbStore, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// History without DB
	hist, err := noDbStore.History(ctx, "any")
	if err != nil || hist != nil {
		t.Errorf("expected nil history without DB, got %v, %v", hist, err)
	}

	// Rollback without DB
	err = noDbStore.Rollback(ctx, "any", 1, "msg")
	if err == nil || !strings.Contains(err.Error(), "requires database") {
		t.Errorf("expected requires database error, got %v", err)
	}

	// New with default root dir
	defaultStore, err := New("")
	if err != nil || defaultStore == nil {
		t.Fatalf("New with empty rootDir failed: %v", err)
	}

	// Patch new file (doesn't exist yet)
	store, _ := setupTestStore(t)
	changed, err := store.Patch(ctx, "brand_new.yaml", "create in patch", func(d *ports.Document) (bool, error) {
		d.Data["created"] = "yes"
		return true, nil
	})
	if err != nil || !changed {
		t.Fatalf("Patch brand new file failed: changed=%v, err=%v", changed, err)
	}
	doc, err := store.Get(ctx, "brand_new.yaml")
	if err != nil || doc.Data["created"] != "yes" {
		t.Fatalf("unexpected brand_new doc: %+v, err=%v", doc, err)
	}

	// Patch returning error
	_, err = store.Patch(ctx, "brand_new.yaml", "err", func(d *ports.Document) (bool, error) {
		return false, context.Canceled
	})
	if err == nil {
		t.Fatal("expected error from Patch when mutate fails")
	}

	// Rollback to deleted revision (content == nil)
	_ = store.Delete(ctx, "brand_new.yaml", "deleted brand_new")
	revs, _ := store.History(ctx, "brand_new.yaml")
	// The delete revision has content == nil
	var delRevID int64
	for _, r := range revs {
		if r.Content == nil {
			delRevID = r.ID
			break
		}
	}
	if delRevID > 0 {
		// Re-create the file
		_ = store.Put(ctx, "brand_new.yaml", ports.Document{Data: map[string]string{"v": "3"}}, "v3")
		// Rollback to deletion
		if err := store.Rollback(ctx, "brand_new.yaml", delRevID, "rollback to delete"); err != nil {
			t.Fatalf("rollback to delete failed: %v", err)
		}
		_, err = store.Get(ctx, "brand_new.yaml")
		if !os.IsNotExist(err) {
			t.Errorf("expected file deleted after rollback, got %v", err)
		}
	}
}

func TestLocalStateStore_MoreErrorBranches(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// 1. New fails when rootDir cannot be created (e.g. rootDir is a regular file)
	filePath := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("file"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(filePath); err == nil {
		t.Error("expected error from New when rootDir is a file")
	}

	// 2. New fails when migrate fails (closed DB)
	db, err := sql.Open("sqlite", filepath.Join(dir, "closed.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if _, err := New(filepath.Join(dir, "state"), WithDB(db)); err == nil {
		t.Error("expected error from New when DB is closed during migrate")
	}

	// 3. Get returns Document{Raw: raw} for bad yaml or empty yaml
	store, _ := setupTestStore(t)
	if err := store.Put(ctx, "empty.yaml", ports.Document{Raw: []byte("")}, "empty"); err != nil {
		t.Fatal(err)
	}
	docEmpty, err := store.Get(ctx, "empty.yaml")
	if err != nil || docEmpty.Data != nil {
		t.Errorf("Get empty.yaml unexpected: doc=%+v, err=%v", docEmpty, err)
	}

	if err := store.Put(ctx, "bad.yaml", ports.Document{Raw: []byte(": bad: yaml")}, "bad"); err != nil {
		t.Fatal(err)
	}
	docBad, err := store.Get(ctx, "bad.yaml")
	if err != nil || docBad.Data != nil {
		t.Errorf("Get bad.yaml unexpected: doc=%+v, err=%v", docBad, err)
	}

	// 4. Patch error when Get fails with non-NotExist error (e.g. invalid relative path)
	if _, err := store.Patch(ctx, "../escape", "msg", func(*ports.Document) (bool, error) { return true, nil }); err == nil {
		t.Error("expected error from Patch with invalid relative path")
	}

	// 5. Rollback errors
	// a) Revision not found
	if err := store.Rollback(ctx, "empty.yaml", 999999, "rollback nonexistent"); err == nil {
		t.Error("expected error from Rollback when revision does not exist")
	}

	// b) Rollback with raw non-YAML content
	if err := store.Put(ctx, "raw.txt", ports.Document{Raw: []byte("plain text content")}, "raw v1"); err != nil {
		t.Fatal(err)
	}
	revs, _ := store.History(ctx, "raw.txt")
	if len(revs) > 0 {
		rawRevID := revs[len(revs)-1].ID
		_ = store.Put(ctx, "raw.txt", ports.Document{Raw: []byte("v2")}, "raw v2")
		if err := store.Rollback(ctx, "raw.txt", rawRevID, "rollback raw"); err != nil {
			t.Errorf("Rollback raw content failed: %v", err)
		}
		gotRaw, err := store.Get(ctx, "raw.txt")
		if err != nil || string(gotRaw.Raw) != "plain text content" {
			t.Errorf("got raw=%q, want 'plain text content', err=%v", gotRaw.Raw, err)
		}
	}

	// 6. New with empty string defaults to current directory
	adapterEmpty, err := New("")
	if err != nil || adapterEmpty == nil {
		t.Fatalf("New('') failed: %v", err)
	}

	// 7. Put fails if parent dir is a file
	store7, _ := setupTestStore(t)
	fullRoot := store7.rootDir
	fileAsParent := filepath.Join(fullRoot, "blocked-file")
	if err := os.WriteFile(fileAsParent, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := store7.Put(ctx, "blocked-file/child.yaml", ports.Document{Data: map[string]string{"a": "b"}}, "msg"); err == nil {
		t.Error("expected Put to fail when parent dir is a file")
	}

	// 8. Put fails if target file is a directory
	dirAsTarget := filepath.Join(fullRoot, "target-is-dir")
	if err := os.MkdirAll(dirAsTarget, 0755); err != nil {
		t.Fatal(err)
	}
	if err := store7.Put(ctx, "target-is-dir", ports.Document{Data: map[string]string{"a": "b"}}, "msg"); err == nil {
		t.Error("expected Put to fail when target path is a directory")
	}

	// 9. Patch fails if Put fails
	if _, err := store7.Patch(ctx, "target-is-dir", "msg", func(d *ports.Document) (bool, error) {
		d.Data = map[string]string{"k": "v"}
		return true, nil
	}); err == nil {
		t.Error("expected Patch to fail when Put fails")
	}

	// 10. PutTree fails if dirPath is a file
	if err := store7.PutTree(ctx, "blocked-file", map[string]ports.Document{
		"f.yaml": {Data: map[string]string{"k": "v"}},
	}, "msg"); err == nil {
		t.Error("expected PutTree to fail when dirPath is a file")
	}

	// 11. PutTree fails if targetFile is a directory
	if err := store7.PutTree(ctx, "ptree", map[string]ports.Document{
		"sub": {Data: map[string]string{"k": "v"}},
	}, "msg"); err != nil {
		t.Fatal(err)
	}
	// Make sub a directory
	ptreeSub := filepath.Join(fullRoot, "ptree", "sub")
	_ = os.Remove(ptreeSub)
	_ = os.MkdirAll(ptreeSub, 0755)
	if err := store7.PutTree(ctx, "ptree", map[string]ports.Document{
		"sub": {Data: map[string]string{"k": "v"}},
	}, "msg"); err == nil {
		t.Error("expected PutTree to fail when targetFile is a directory")
	}

	// 12. History fails if DB query fails (e.g. DB closed)
	store12, db12 := setupTestStore(t)
	_ = db12.Close()
	if _, err := store12.History(ctx, "any.yaml"); err == nil {
		t.Error("expected History to fail when DB is closed")
	}
}


