package modpack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type ImportedWorld struct {
	Name      string
	MCVersion string
	Loader    string // "fabric" or "neoforge"
	Source    string // "modpack" or "modlist"
	Slugs     []string
	RawMods   string
}

// ParseMrpack reads a Modrinth .mrpack archive and extracts world configuration and mods.
func ParseMrpack(r io.ReaderAt, size int64) (*ImportedWorld, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	var indexFile *zip.File
	for _, f := range zr.File {
		if f.Name == "modrinth.index.json" {
			indexFile = f
			break
		}
	}
	if indexFile == nil {
		return nil, fmt.Errorf("modrinth.index.json not found in mrpack")
	}

	rc, err := indexFile.Open()
	if err != nil {
		return nil, fmt.Errorf("open modrinth.index.json: %w", err)
	}
	defer rc.Close()

	var idx MrpackIndex
	if err := json.NewDecoder(rc).Decode(&idx); err != nil {
		return nil, fmt.Errorf("decode modrinth.index.json: %w", err)
	}

	world := &ImportedWorld{
		Name:      idx.Name,
		MCVersion: idx.Dependencies["minecraft"],
		Source:    "modpack",
	}

	if idx.Dependencies["fabric-loader"] != "" {
		world.Loader = "fabric"
	} else if idx.Dependencies["neoforge"] != "" || idx.Dependencies["forge"] != "" {
		world.Loader = "neoforge"
	} else {
		world.Loader = "neoforge"
	}

	var slugs []string
	for _, f := range idx.Files {
		// Clean file path (e.g. "mods/jei-1.21.1.jar" -> "jei")
		base := filepath.Base(f.Path)
		base = strings.TrimSuffix(base, ".jar")
		if idx := strings.Index(base, "-"); idx != -1 {
			base = base[:idx]
		}
		if base != "" {
			slugs = append(slugs, base)
		}
	}
	world.Slugs = slugs
	world.RawMods = strings.Join(slugs, "\n")
	return world, nil
}

type mmcComponent struct {
	CachedName    string `json:"cachedName"`
	CachedVersion string `json:"cachedVersion"`
	Important     bool   `json:"important"`
	UID           string `json:"uid"`
	Version       string `json:"version"`
}

type mmcPack struct {
	Components    []mmcComponent `json:"components"`
	FormatVersion int            `json:"formatVersion"`
}

// ParsePrismZip reads a Prism/MultiMC instance zip archive.
func ParsePrismZip(r io.ReaderAt, size int64) (*ImportedWorld, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	world := &ImportedWorld{
		Name:   "Imported Prism World",
		Loader: "neoforge",
		Source: "modlist",
	}

	var foundMMC bool
	for _, f := range zr.File {
		clean := filepath.Clean(f.Name)
		if strings.HasSuffix(clean, "mmc-pack.json") {
			rc, err := f.Open()
			if err == nil {
				var pack mmcPack
				if err := json.NewDecoder(rc).Decode(&pack); err == nil {
					foundMMC = true
					for _, c := range pack.Components {
						switch c.UID {
						case "net.minecraft":
							world.MCVersion = c.Version
						case "net.fabricmc.fabric-loader":
							world.Loader = "fabric"
						case "net.neoforged", "net.minecraftforge":
							world.Loader = "neoforge"
						}
					}
				}
				rc.Close()
			}
		} else if strings.HasSuffix(clean, "instance.cfg") {
			rc, err := f.Open()
			if err == nil {
				buf := new(bytes.Buffer)
				_, _ = buf.ReadFrom(rc)
				for line := range strings.SplitSeq(buf.String(), "\n") {
					line = strings.TrimSpace(line)
					if after, ok := strings.CutPrefix(line, "name="); ok {
						world.Name = after
					}
				}
				rc.Close()
			}
		}
	}

	var slugs []string
	for _, f := range zr.File {
		if strings.Contains(f.Name, "mods/") && strings.HasSuffix(f.Name, ".jar") {
			base := filepath.Base(f.Name)
			base = strings.TrimSuffix(base, ".jar")
			if idx := strings.Index(base, "-"); idx != -1 {
				base = base[:idx]
			}
			if base != "" {
				slugs = append(slugs, base)
			}
		}
	}
	world.Slugs = slugs
	world.RawMods = strings.Join(slugs, "\n")

	if !foundMMC && len(slugs) == 0 {
		return nil, fmt.Errorf("unrecognized Prism instance format (missing mmc-pack.json or mods)")
	}

	return world, nil
}

// ParseRawModList reads a plain text mod list (e.g. mods.txt format).
func ParseRawModList(text string) *ImportedWorld {
	world := &ImportedWorld{
		Name:    "Imported Modded World",
		Loader:  "neoforge",
		Source:  "modlist",
		RawMods: text,
	}

	lines := strings.Split(text, "\n")
	var slugs []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if after, ok := strings.CutPrefix(line, "# Modpack:"); ok {
			world.Name = strings.TrimSpace(after)
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		slug := strings.TrimSuffix(line, "?")
		if slug != "" {
			slugs = append(slugs, slug)
		}
	}
	world.Slugs = slugs
	return world
}

// ParseR2Z reads an r2modman / Thunderstore .r2z or zip containing export.r2x or manifest.json.
func ParseR2Z(r io.ReaderAt, size int64) (*ImportedWorld, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	var manifestFile *zip.File
	for _, f := range zr.File {
		if f.Name == "export.r2x" || f.Name == "manifest.json" {
			manifestFile = f
			break
		}
	}
	if manifestFile == nil {
		return nil, fmt.Errorf("neither export.r2x nor manifest.json found in profile")
	}

	rc, err := manifestFile.Open()
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer rc.Close()

	if manifestFile.Name == "export.r2x" {
		var ef exportFormat
		if err := yaml.NewDecoder(rc).Decode(&ef); err != nil {
			return nil, fmt.Errorf("decode export.r2x: %w", err)
		}
		world := &ImportedWorld{
			Name:   ef.ProfileName,
			Source: "modpack",
		}
		var slugs []string
		for _, m := range ef.Mods {
			if !m.Enabled {
				continue
			}
			slug := m.Name
			slugs = append(slugs, slug)
		}
		world.Slugs = slugs
		world.RawMods = strings.Join(slugs, "\n")
		return world, nil
	}

	var tsManifest struct {
		Name         string   `json:"name"`
		Dependencies []string `json:"dependencies"`
	}
	if err := json.NewDecoder(rc).Decode(&tsManifest); err != nil {
		return nil, fmt.Errorf("decode manifest.json: %w", err)
	}
	world := &ImportedWorld{
		Name:    tsManifest.Name,
		Source:  "modpack",
		Slugs:   tsManifest.Dependencies,
		RawMods: strings.Join(tsManifest.Dependencies, "\n"),
	}
	return world, nil
}
