package backups

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"agrelha/internal/domain"
)

var safeBackupName = regexp.MustCompile(`^(mc|valheim)-[a-z0-9-]+-\d{2}-[a-zA-Z0-9_.-]+\.tar\.gz$`)

// FormatBackupFileName generates a standard archive name for instance backups.
func FormatBackupFileName(slug string, num int, tag string) string {
	return domain.FormatBackupFileName(slug, num, tag)
}

// PruneBackups keeps the latest keepCount backups matching the instance pattern and deletes older archives.
func PruneBackups(backupsDir, slug string, num, keepCount int) error {
	if backupsDir == "" || keepCount <= 0 {
		return nil
	}

	patternMC := filepath.Join(backupsDir, fmt.Sprintf("mc-%s-%02d-*.tar.gz", slug, num))
	patternValheim := filepath.Join(backupsDir, fmt.Sprintf("valheim-%s-%02d-*.tar.gz", slug, num))
	matchesMC, err := filepath.Glob(patternMC)
	if err != nil {
		return err
	}
	matchesValheim, err := filepath.Glob(patternValheim)
	if err != nil {
		return err
	}
	matches := append(matchesMC, matchesValheim...)

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
