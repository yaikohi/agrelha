package backups

import (
	"os"
	"path/filepath"
	"testing"
)

func seedConfigs(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"zoldout.cfg":         "[General]\nenabled = true\n",
		"adventure.cfg":       "[Main]\nrate = 2\n",
		"bepinex.cfg.default": "ignored\n",
		"notes.txt":           "ignored\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLocalSnapshotReadsConfigsStraightOffDisk(t *testing.T) {
	snap, err := ReadConfigSnapshotFromDir(seedConfigs(t), "odin-01")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if snap == nil {
		t.Fatal("a Host with configs on disk must produce a snapshot without a publishing sidecar")
	}
	if len(snap.Files) != 2 {
		t.Fatalf("want 2 .cfg files, got %d: %+v", len(snap.Files), snap.Files)
	}
	if snap.Files[0].Name != "adventure.cfg" {
		t.Errorf("files must be sorted, got %q first", snap.Files[0].Name)
	}
	for _, f := range snap.Files {
		if f.SHA256 == "" || f.Size == 0 {
			t.Errorf("%s has no checksum or size", f.Name)
		}
	}
	if snap.PublishedAt.IsZero() {
		t.Error("snapshot must carry a timestamp so the UI can say how fresh it is")
	}
}

func TestAbsentDirectoryIsNotAnError(t *testing.T) {
	snap, err := ReadConfigSnapshotFromDir(filepath.Join(t.TempDir(), "nope"), "x")
	if err != nil || snap != nil {
		t.Errorf("a world that has not generated configs yet is not an error: snap=%v err=%v", snap, err)
	}
	if snap, err := ReadConfigSnapshotFromDir("", "x"); err != nil || snap != nil {
		t.Errorf("no data root configured must be silent: snap=%v err=%v", snap, err)
	}
}

func TestLocalFileReadRefusesTraversal(t *testing.T) {
	dir := seedConfigs(t)
	if _, _, err := ReadConfigFileFromDir(dir, "../secrets"); err == nil {
		t.Error("a traversing name must be refused")
	}
	body, sum, err := ReadConfigFileFromDir(dir, "zoldout.cfg")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if body == "" || sum == "" {
		t.Error("expected content and a checksum")
	}
}
