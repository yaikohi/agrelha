package backups

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"agrelha/internal/domain"
)

// configsDir is where the config-publish sidecar stages an Instance's
// Generated configs on the shared NFS export, beside server-build.json. It
// writes index.json last, so a directory without one is a publish in progress
// and is treated as not published rather than as an empty config set.
const (
	configsDir     = "bepinex-configs"
	configIndexDoc = "index.json"
)

type configIndexFile struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Modified string `json:"modified"`
}

type configIndexDocument struct {
	Instance    string            `json:"instance"`
	PublishedAt string            `json:"published_at"`
	Files       []configIndexFile `json:"files"`
}

// ReadConfigSnapshot reports the Generated configs the sidecar last published
// for an Instance. A missing directory or index is not an error - the world may
// never have started, or the sidecar may be mid-publish - and returns nil.
func ReadConfigSnapshot(backupsDir, slug string, num int) (*domain.ConfigSnapshot, error) {
	if backupsDir == "" || slug == "" {
		return nil, nil
	}
	path := filepath.Join(instanceDir(backupsDir, slug, num), configsDir, configIndexDoc)
	raw, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return nil, nil
	case err != nil:
		return nil, err
	}

	var doc configIndexDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	out := &domain.ConfigSnapshot{Instance: doc.Instance}
	if t, err := time.Parse(time.RFC3339, doc.PublishedAt); err == nil {
		out.PublishedAt = t
	}
	for _, f := range doc.Files {
		// A name that is not a plain file name would let the index steer reads
		// out of the snapshot directory. The sidecar never writes one; refusing
		// here means a tampered index cannot.
		if !safeConfigName(f.Name) {
			continue
		}
		sf := domain.SnapshotFile{Name: f.Name, Size: f.Size, SHA256: f.SHA256}
		if t, err := time.Parse(time.RFC3339, f.Modified); err == nil {
			sf.Modified = t
		}
		out.Files = append(out.Files, sf)
	}
	return out, nil
}

// ReadConfigFile returns one published Generated config and the digest of the
// bytes actually read.
//
// The digest is verified against the index rather than trusted from it: the
// export is NFS and the sidecar republishes whenever a mod rewrites a file, so
// reading a file that has moved since the index was written is a real race, not
// a theoretical one. Callers use the returned digest for the optimistic
// concurrency check when saving.
func ReadConfigFile(backupsDir, slug string, num int, name string) (string, string, error) {
	if backupsDir == "" || slug == "" {
		return "", "", os.ErrNotExist
	}
	if !safeConfigName(name) {
		return "", "", fmt.Errorf("invalid config file name %q", name)
	}
	path := filepath.Join(instanceDir(backupsDir, slug, num), configsDir, name)
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(raw)
	return string(raw), hex.EncodeToString(sum[:]), nil
}

// safeConfigName accepts only a plain file name - no directory separators, no
// parent references, no leading dot.
func safeConfigName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if name != filepath.Base(name) {
		return false
	}
	if filepath.IsAbs(name) {
		return false
	}
	if name[0] == '.' {
		return false
	}
	for i := 0; i < len(name); i++ {
		if name[i] == '/' || name[i] == '\\' || name[i] == 0 {
			return false
		}
	}
	return true
}
