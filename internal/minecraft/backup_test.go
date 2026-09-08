package minecraft

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
