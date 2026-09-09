package backups

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var safeBackupName = regexp.MustCompile(`^mc-[a-z0-9-]+-\d{2}-[a-zA-Z0-9_-]+\.tar\.gz$`)

// FormatBackupFileName generates a standard archive name for instance backups.
func FormatBackupFileName(slug string, num int, tag string) string {
	ts := time.Now().UTC().Format("20060102-150405")
	if tag != "" {
		return fmt.Sprintf("mc-%s-%02d-%s-%s.tar.gz", slug, num, tag, ts)
	}
	return fmt.Sprintf("mc-%s-%02d-%s.tar.gz", slug, num, ts)
}

// PruneBackups keeps the latest keepCount backups matching the instance pattern and deletes older archives.
func PruneBackups(backupsDir, slug string, num, keepCount int) error {
	if backupsDir == "" || keepCount <= 0 {
		return nil
	}

	pattern := filepath.Join(backupsDir, fmt.Sprintf("mc-%s-%02d-*.tar.gz", slug, num))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}

	if len(matches) <= keepCount {
		return nil
	}

	type fileInfo struct {
		path    string
		modTime time.Time
	}

	files := make([]fileInfo, 0, len(matches))
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil {
			files = append(files, fileInfo{path: m, modTime: fi.ModTime()})
		}
	}

	// Sort newest first
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	// Remove all files past keepCount
	for i := keepCount; i < len(files); i++ {
		_ = os.Remove(files[i].path)
	}

	return nil
}

// DeleteBackup safely deletes a specific backup archive file from backupsDir.
func DeleteBackup(backupsDir, archiveName string) error {
	cleanName := filepath.Base(strings.TrimSpace(archiveName))
	if !safeBackupName.MatchString(cleanName) {
		return fmt.Errorf("invalid or unauthorized backup file name %q", cleanName)
	}

	targetPath := filepath.Join(backupsDir, cleanName)
	return os.Remove(targetPath)
}
