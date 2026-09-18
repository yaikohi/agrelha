package backups

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"agrelha/internal/domain"
)

// serverBuildFile is what the build-watch sidecar writes into each Instance's
// backup directory on the shared NFS export. agrelha mounts that export
// read-only, so this is the whole contract between them.
const serverBuildFile = "server-build.json"

type serverBuildDoc struct {
	Instance  string `json:"instance"`
	Installed string `json:"installed"`
	Latest    string `json:"latest"`
	CheckedAt string `json:"checked_at"`
}

// ReadServerBuild reports the build the Instance is running and the newest the
// store offers. A missing file is not an error: the sidecar may not have run
// yet, and "we do not know" is a legitimate answer that the UI must be able to
// tell apart from "up to date".
func ReadServerBuild(backupsDir, slug string, num int) (*domain.ServerBuild, error) {
	if backupsDir == "" || slug == "" {
		return nil, nil
	}
	path := filepath.Join(backupsDir, fmt.Sprintf("%s-%02d", slug, num), serverBuildFile)
	raw, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return nil, nil
	case err != nil:
		return nil, err
	}

	var doc serverBuildDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	out := &domain.ServerBuild{Installed: doc.Installed, Latest: doc.Latest}
	if t, err := time.Parse(time.RFC3339, doc.CheckedAt); err == nil {
		out.CheckedAt = t
	}
	return out, nil
}
