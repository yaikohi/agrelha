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
	curseforgeManifest = ".curseforge-manifest.json"
	neoforgeManifest   = ".neoforge-manifest.json"
	forgeManifest      = ".forge-manifest.json"
	fabricManifest     = ".fabric-manifest.json"
	dataVolume         = "data"
)

type Reader struct {
	dataRoot string
}

func New(dataRoot string) *Reader { return &Reader{dataRoot: dataRoot} }

var (
	_ ports.InstalledBuilds = (*Reader)(nil)
	_ ports.PackReader      = (*Reader)(nil)
)

type forgeManifestFile struct {
	MinecraftVersion string `json:"minecraftVersion"`
}

type fabricManifestFile struct {
	Origin struct {
		Game string `json:"game"`
	} `json:"origin"`
}

// curseforgeManifestFile is the installer's record of the Pack it unpacked. It
// also lists every file of the Pack, which is deliberately not decoded: it runs
// to four figures on a big modpack and names nothing a player needs.
type curseforgeManifestFile struct {
	ModID          int    `json:"modId"`
	FileID         int    `json:"fileId"`
	FileName       string `json:"fileName"`
	ModpackName    string `json:"modpackName"`
	ModpackVersion string `json:"modpackVersion"`
}

// InstalledPack reads which Pack release this server installed.
//
// shortcut: CurseForge packs only - a Modrinth Pack world has no such manifest
// and falls back to its pack link. Add the Modrinth installer's record here
// when a world needs it.
func (r *Reader) InstalledPack(ctx context.Context, id domain.InstanceID) (ports.InstalledPack, error) {
	if r == nil || r.dataRoot == "" {
		return ports.InstalledPack{}, ports.ErrNotImplemented
	}

	b, err := os.ReadFile(filepath.Join(r.dataDir(id), curseforgeManifest))
	if err != nil {
		return ports.InstalledPack{}, err
	}
	var m curseforgeManifestFile
	if err := json.Unmarshal(b, &m); err != nil {
		return ports.InstalledPack{}, fmt.Errorf("installed: %s: %w", curseforgeManifest, err)
	}
	if m.ModID <= 0 || m.FileID <= 0 {
		return ports.InstalledPack{}, fmt.Errorf("installed: %s names no pack file: %w", curseforgeManifest, os.ErrNotExist)
	}
	return ports.InstalledPack{
		Provider: domain.ProviderCurseForge,
		ModID:    m.ModID,
		FileID:   m.FileID,
		Name:     m.ModpackName,
		Version:  m.ModpackVersion,
		FileName: m.FileName,
	}, nil
}

func (r *Reader) dataDir(id domain.InstanceID) string {
	return filepath.Join(
		composereconcile.InstanceDataDir(r.dataRoot, id, ""),
		dataVolume,
	)
}

func (r *Reader) InstalledBuild(ctx context.Context, id domain.InstanceID) (domain.PackFacts, error) {
	if r == nil || r.dataRoot == "" {
		return domain.PackFacts{}, ports.ErrNotImplemented
	}
	dir := r.dataDir(id)

	if b, err := os.ReadFile(filepath.Join(dir, neoforgeManifest)); err == nil {
		var m forgeManifestFile
		if err := json.Unmarshal(b, &m); err != nil {
			return domain.PackFacts{}, fmt.Errorf("installed: %s: %w", neoforgeManifest, err)
		}
		return domain.PackFacts{Loader: domain.LoaderNeoForge, MCVersion: m.MinecraftVersion}, nil
	}

	if b, err := os.ReadFile(filepath.Join(dir, forgeManifest)); err == nil {
		var m forgeManifestFile
		if err := json.Unmarshal(b, &m); err != nil {
			return domain.PackFacts{}, fmt.Errorf("installed: %s: %w", forgeManifest, err)
		}
		return domain.PackFacts{Loader: domain.LoaderForge, MCVersion: m.MinecraftVersion}, nil
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
