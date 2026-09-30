package backups

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func snapshotDir(t *testing.T) (backupsDir, snapDir string) {
	t.Helper()
	backupsDir = t.TempDir()
	snapDir = filepath.Join(backupsDir, "boppo-02", "bepinex-configs")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return backupsDir, snapDir
}

func publish(t *testing.T, snapDir string, files map[string]string) {
	t.Helper()
	doc := configIndexDocument{
		Instance:    "boppo-02",
		PublishedAt: time.Now().UTC().Format(time.RFC3339),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(snapDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(body))
		doc.Files = append(doc.Files, configIndexFile{
			Name:     name,
			Size:     int64(len(body)),
			SHA256:   hex.EncodeToString(sum[:]),
			Modified: time.Now().UTC().Format(time.RFC3339),
		})
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapDir, configIndexDoc), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNoSnapshotClaimsNothing(t *testing.T) {
	snap, err := ReadConfigSnapshot(t.TempDir(), "boppo", 2)
	if err != nil {
		t.Fatalf("a missing snapshot is not an error: %v", err)
	}
	if snap.Known() {
		t.Error("nothing published, so nothing should be claimed")
	}
	if snap.Len() != 0 {
		t.Error("nil snapshot must report no files")
	}
}

func TestSnapshotWithoutAnIndexIsTreatedAsUnpublished(t *testing.T) {
	backupsDir, snapDir := snapshotDir(t)
	if err := os.WriteFile(filepath.Join(snapDir, "a.cfg"), []byte("[S]\nK = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := ReadConfigSnapshot(backupsDir, "boppo", 2)
	if err != nil {
		t.Fatalf("mid-publish is not an error: %v", err)
	}
	if snap.Known() {
		t.Error("without index.json the snapshot is not yet readable")
	}
}

func TestSnapshotListsWhatWasPublished(t *testing.T) {
	backupsDir, snapDir := snapshotDir(t)
	publish(t, snapDir, map[string]string{
		"org.bepinex.plugins.mining.cfg": "[2 - Mining]\nMining Yield Factor = 3\n",
		"DustBegone.Valheim.cfg":         "[General]\nEnabled = true\n",
	})

	snap, err := ReadConfigSnapshot(backupsDir, "boppo", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Known() || snap.Len() != 2 {
		t.Fatalf("want 2 published files, got %d", snap.Len())
	}
	if snap.Instance != "boppo-02" {
		t.Errorf("instance = %q", snap.Instance)
	}
	if snap.PublishedAt.IsZero() {
		t.Error("published_at should have parsed")
	}
	f, ok := snap.Lookup("org.bepinex.plugins.mining.cfg")
	if !ok {
		t.Fatal("mining config not listed")
	}
	if f.SHA256 == "" || f.Size == 0 || f.Modified.IsZero() {
		t.Errorf("incomplete file entry: %+v", f)
	}
	if _, ok := snap.Lookup("not-there.cfg"); ok {
		t.Error("Lookup must not invent entries")
	}
}

func TestReadConfigFileReturnsTheDigestOfWhatItActuallyRead(t *testing.T) {
	backupsDir, snapDir := snapshotDir(t)
	body := "[2 - Mining]\nMining Yield Factor = 3\n"
	publish(t, snapDir, map[string]string{"mining.cfg": body})

	got, digest, err := ReadConfigFile(backupsDir, "boppo", 2, "mining.cfg")
	if err != nil {
		t.Fatal(err)
	}
	if got != body {
		t.Errorf("body = %q", got)
	}
	sum := sha256.Sum256([]byte(body))
	if digest != hex.EncodeToString(sum[:]) {
		t.Error("digest must be of the bytes read, not copied from the index")
	}

	if err := os.WriteFile(filepath.Join(snapDir, "mining.cfg"), []byte(body+"Extra = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, after, err := ReadConfigFile(backupsDir, "boppo", 2, "mining.cfg")
	if err != nil {
		t.Fatal(err)
	}
	if after == digest {
		t.Error("a rewritten file must produce a different digest")
	}
}

func TestSnapshotRefusesToReadOutsideItself(t *testing.T) {
	backupsDir, snapDir := snapshotDir(t)
	publish(t, snapDir, map[string]string{"ok.cfg": "[S]\nK = 1\n"})

	for _, name := range []string{
		"../server-build.json",
		"../../etc/passwd",
		"/etc/passwd",
		"sub/dir.cfg",
		"",
		".",
		"..",
		".hidden.cfg",
	} {
		if _, _, err := ReadConfigFile(backupsDir, "boppo", 2, name); err == nil {
			t.Errorf("%q should have been refused", name)
		}
	}
}

func TestSnapshotIndexCannotSmuggleAPath(t *testing.T) {
	backupsDir, snapDir := snapshotDir(t)
	doc := configIndexDocument{
		Instance:    "boppo-02",
		PublishedAt: time.Now().UTC().Format(time.RFC3339),
		Files: []configIndexFile{
			{Name: "../../etc/passwd", Size: 1, SHA256: "x"},
			{Name: "fine.cfg", Size: 1, SHA256: "y"},
		},
	}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(snapDir, configIndexDoc), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	snap, err := ReadConfigSnapshot(backupsDir, "boppo", 2)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Len() != 1 {
		t.Fatalf("want only the safe entry, got %d", snap.Len())
	}
	if snap.Files[0].Name != "fine.cfg" {
		t.Errorf("kept the wrong entry: %q", snap.Files[0].Name)
	}
}

func TestUnreadableIndexIsAnError(t *testing.T) {
	backupsDir, snapDir := snapshotDir(t)
	if err := os.WriteFile(filepath.Join(snapDir, configIndexDoc), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadConfigSnapshot(backupsDir, "boppo", 2); err == nil {
		t.Error("a corrupt index must be reported, not silently read as empty")
	}
}

func TestSnapshotNeedsABackupsDirAndSlug(t *testing.T) {
	if snap, err := ReadConfigSnapshot("", "boppo", 2); err != nil || snap != nil {
		t.Error("no backups dir configured is not an error, just nothing to read")
	}
	if snap, err := ReadConfigSnapshot(t.TempDir(), "", 2); err != nil || snap != nil {
		t.Error("no slug is not an error either")
	}
	if _, _, err := ReadConfigFile("", "boppo", 2, "x.cfg"); err == nil {
		t.Error("reading a file with no backups dir must fail")
	}
}

func TestBothReadersAgreeOnTheInstanceDirectory(t *testing.T) {
	backupsDir, snapDir := snapshotDir(t)
	publish(t, snapDir, map[string]string{"a.cfg": "[S]\nK = 1\n"})

	buildDoc := `{"instance":"boppo-02","installed":"1","latest":"2","checked_at":"2026-09-28T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(backupsDir, "boppo-02", serverBuildFile), []byte(buildDoc), 0o644); err != nil {
		t.Fatal(err)
	}

	build, err := ReadServerBuild(backupsDir, "boppo", 2)
	if err != nil || build == nil {
		t.Fatalf("server build not found in the same directory: %v", err)
	}
	snap, err := ReadConfigSnapshot(backupsDir, "boppo", 2)
	if err != nil || !snap.Known() {
		t.Fatalf("snapshot not found in the same directory: %v", err)
	}
}
