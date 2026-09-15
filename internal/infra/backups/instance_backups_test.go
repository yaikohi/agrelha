package backups

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFormatBackupFileName(t *testing.T) {
	name1 := FormatBackupFileName("fluxweave", 1, "")
	if filepath.Ext(name1) != ".gz" {
		t.Errorf("expected .tar.gz extension, got %s", name1)
	}

	name2 := FormatBackupFileName("fluxweave", 1, "stop")
	if !safeBackupName.MatchString(name2) {
		t.Errorf("name %s failed safe backup regex", name2)
	}
}

func TestPruneBackups(t *testing.T) {
	dir := t.TempDir()

	// Create 7 files with increasing timestamps
	baseTime := time.Now().Add(-10 * time.Hour)
	for i := 1; i <= 7; i++ {
		fn := filepath.Join(dir, FormatBackupFileName("ducktopia", 2, ""))
		// Slight offset
		fn = filepath.Join(dir, "mc-ducktopia-02-test"+string(rune('a'+i))+".tar.gz")
		if err := os.WriteFile(fn, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
		modTime := baseTime.Add(time.Duration(i) * time.Hour)
		_ = os.Chtimes(fn, modTime, modTime)
	}

	// Prune down to 5
	if err := PruneBackups(dir, "ducktopia", 2, 5); err != nil {
		t.Fatal(err)
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "mc-ducktopia-02-*.tar.gz"))
	if len(matches) != 5 {
		t.Fatalf("expected 5 files after pruning, got %d", len(matches))
	}
}

func TestDeleteBackup(t *testing.T) {
	dir := t.TempDir()
	validName := "mc-ducktopia-01-20260101-120000.tar.gz"
	filePath := filepath.Join(dir, validName)
	_ = os.WriteFile(filePath, []byte("test"), 0644)

	// Valid delete
	if err := DeleteBackup(dir, validName); err != nil {
		t.Errorf("expected delete to succeed, got %v", err)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("expected file to be deleted")
	}

	// Invalid / path traversal rejection
	if err := DeleteBackup(dir, "../dangerous.tar.gz"); err == nil {
		t.Error("expected traversal to fail safe check")
	}
	if err := DeleteBackup(dir, "random-file.txt"); err == nil {
		t.Error("expected non-tar.gz to fail safe check")
	}
}

func TestStat(t *testing.T) {
	dir := t.TempDir()

	// Empty dir
	info, err := Stat(dir)
	if err != nil {
		t.Fatalf("Stat empty dir failed: %v", err)
	}
	if info.Count != 0 || info.TotalSize != 0 {
		t.Errorf("expected 0 count and size, got %+v", info)
	}

	// Dir with subfolder and 2 files
	_ = os.Mkdir(filepath.Join(dir, "subfolder"), 0755)
	f1 := filepath.Join(dir, "file1.tar.gz")
	f2 := filepath.Join(dir, "file2.tar.gz")
	_ = os.WriteFile(f1, []byte("12345"), 0644)
	_ = os.WriteFile(f2, []byte("1234567890"), 0644)
	now := time.Now()
	_ = os.Chtimes(f1, now.Add(-time.Hour), now.Add(-time.Hour))
	_ = os.Chtimes(f2, now, now)

	info, err = Stat(dir)
	if err != nil {
		t.Fatalf("Stat populated dir failed: %v", err)
	}
	if info.Count != 2 || info.TotalSize != 15 || info.LatestName != "file2.tar.gz" {
		t.Errorf("Stat unexpected info: %+v", info)
	}

	// Non-existent dir returns error
	_, err = Stat(filepath.Join(dir, "does_not_exist"))
	if err == nil {
		t.Error("Stat on nonexistent dir want error, got nil")
	}
}

func TestPruneBackups_EdgeCases(t *testing.T) {
	// Empty backupsDir
	if err := PruneBackups("", "test", 1, 5); err != nil {
		t.Errorf("empty backupsDir should return nil, got %v", err)
	}

	// keepCount <= 0
	if err := PruneBackups("/tmp", "test", 1, 0); err != nil {
		t.Errorf("keepCount 0 should return nil, got %v", err)
	}

	// matches <= keepCount
	dir := t.TempDir()
	fn := filepath.Join(dir, "mc-test-01-backup.tar.gz")
	_ = os.WriteFile(fn, []byte("data"), 0644)
	if err := PruneBackups(dir, "test", 1, 5); err != nil {
		t.Errorf("matches <= keepCount should return nil, got %v", err)
	}
}

