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
