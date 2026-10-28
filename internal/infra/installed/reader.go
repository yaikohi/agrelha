// Package installed reads the build a server actually has on disk.
package installed

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"agrelha/internal/domain"
	composereconcile "agrelha/internal/infra/reconcile/compose"
	"agrelha/internal/ports"
)

// The installer writes one of these next to the world when it sets a server up.
// They are the record of what it actually did, so they are read rather than the
// Pack asked about again.
const (
	neoforgeManifest = ".neoforge-manifest.json"
	fabricManifest   = ".fabric-manifest.json"
	dataVolume       = "data"
)

type Reader struct {
	dataRoot string
}

func New(dataRoot string) *Reader { return &Reader{dataRoot: dataRoot} }

var _ ports.InstalledBuilds = (*Reader)(nil)

type forgeManifest struct {
	MinecraftVersion string `json:"minecraftVersion"`
}

type fabricManifestFile struct {
	Origin struct {
		Game string `json:"game"`
	} `json:"origin"`
}

func (r *Reader) InstalledBuild(ctx context.Context, id domain.InstanceID) (domain.PackFacts, error) {
	if r == nil || r.dataRoot == "" {
		return domain.PackFacts{}, ports.ErrNotImplemented
	}
	dir := filepath.Join(
		composereconcile.InstanceDataDir(r.dataRoot, id, ""),
		dataVolume,
	)

	if b, err := os.ReadFile(filepath.Join(dir, neoforgeManifest)); err == nil {
		var m forgeManifest
		if err := json.Unmarshal(b, &m); err != nil {
			return domain.PackFacts{}, fmt.Errorf("installed: %s: %w", neoforgeManifest, err)
		}
		return domain.PackFacts{Loader: domain.LoaderNeoForge, MCVersion: m.MinecraftVersion}, nil
	}

	if b, err := os.ReadFile(filepath.Join(dir, fabricManifest)); err == nil {
		var m fabricManifestFile
		if err := json.Unmarshal(b, &m); err != nil {
			return domain.PackFacts{}, fmt.Errorf("installed: %s: %w", fabricManifest, err)
		}
		return domain.PackFacts{Loader: domain.LoaderFabric, MCVersion: m.Origin.Game}, nil
	}

	return domain.PackFacts{}, fmt.Errorf("no installer manifest under %s: %w", dir, os.ErrNotExist)
}
