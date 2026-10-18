package backups

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"agrelha/internal/domain"
)

func ReadConfigSnapshotFromDir(dir, instance string) (*domain.ConfigSnapshot, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	switch {
	case os.IsNotExist(err):
		return nil, nil
	case err != nil:
		return nil, err
	}

	out := &domain.ConfigSnapshot{Instance: instance}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".cfg") || !safeConfigName(name) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		sum := sha256.Sum256(raw)
		sf := domain.SnapshotFile{Name: name, Size: int64(len(raw)), SHA256: hex.EncodeToString(sum[:])}
		if info, err := e.Info(); err == nil {
			sf.Modified = info.ModTime()
			if sf.Modified.After(out.PublishedAt) {
				out.PublishedAt = sf.Modified
			}
		}
		out.Files = append(out.Files, sf)
	}
	if len(out.Files) == 0 {
		return nil, nil
	}
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Name < out.Files[j].Name })
	return out, nil
}

func ReadConfigFileFromDir(dir, name string) (string, string, error) {
	if dir == "" {
		return "", "", os.ErrNotExist
	}
	if !safeConfigName(name) {
		return "", "", fmt.Errorf("invalid config file name %q", name)
	}
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(raw)
	return string(raw), hex.EncodeToString(sum[:]), nil
}
