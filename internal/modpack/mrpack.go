package modpack

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"agrelha/internal/modrinth"
)

type MrpackIndex struct {
	FormatVersion int               `json:"formatVersion"`
	Game          string            `json:"game"`
	VersionID     string            `json:"versionId"`
	Name          string            `json:"name"`
	Summary       string            `json:"summary,omitempty"`
	Dependencies  map[string]string `json:"dependencies"`
	Files         []MrpackFile      `json:"files"`
}

type MrpackEnv struct {
	Client string `json:"client"`
	Server string `json:"server"`
}

type MrpackFile struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Env       *MrpackEnv        `json:"env,omitempty"`
	Downloads []string          `json:"downloads"`
	FileSize  int64             `json:"fileSize"`
}

type prismMetaVersion struct {
	Version  string `json:"version"`
	Requires []struct {
		Equals string `json:"equals"`
		UID    string `json:"uid"`
	} `json:"requires"`
}

type prismMetaIndex struct {
	Versions []prismMetaVersion `json:"versions"`
}

// ResolveNeoForgeVersion finds the best matching NeoForge release version for a given Minecraft version.
func ResolveNeoForgeVersion(ctx context.Context, mcVersion, requestedVersion string) string {
	requestedVersion = strings.TrimSpace(requestedVersion)
	if requestedVersion != "" && requestedVersion != "recommended" && requestedVersion != "latest" {
		return requestedVersion
	}

	// Try querying Prism Launcher's official NeoForge metadata
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://meta.prismlauncher.org/v1/net.neoforged/index.json", nil)
	if err == nil {
		req.Header.Set("User-Agent", "agrelha/0.9.0 (https://github.com/ykhi/yaya-ops)")
		client := &http.Client{Timeout: 5 * time.Second}
		if resp, err := client.Do(req); err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var idx prismMetaIndex
			if err := json.NewDecoder(resp.Body).Decode(&idx); err == nil {
				for _, v := range idx.Versions {
					for _, r := range v.Requires {
						if r.UID == "net.minecraft" && r.Equals == mcVersion {
							return v.Version
						}
					}
				}
			}
		}
	}

	// Known fallback releases for popular versions
	switch mcVersion {
	case "1.21.1":
		return "21.1.249"
	case "1.21.0", "1.21":
		return "21.0.167"
	case "1.20.6":
		return "20.6.141"
	case "1.20.4":
		return "20.4.237"
	default:
		return "21.1.249"
	}
}

// ModrinthProvider defines the subset of Modrinth client needed for building modpacks.
type ModrinthProvider interface {
	GetProject(ctx context.Context, idOrSlug string) (*modrinth.Project, error)
	GetProjectVersions(ctx context.Context, idOrSlug, mcVersion string) ([]modrinth.Version, error)
}

// BuildMrpack generates a Modrinth modpack (.mrpack) ZIP archive suitable for 1-click import into Prism Launcher.
// It filters out server-only mods (client_side == "unsupported"), resolves client files and hashes,
// and packages declarative mod configs into overrides/config/.
func BuildMrpack(ctx context.Context, mr ModrinthProvider, packName, mcVersion, neoforgeVersion string, slugs []string, configs map[string]string) ([]byte, error) {
	if mcVersion == "" {
		mcVersion = "1.21.1"
	}
	resolvedNF := ResolveNeoForgeVersion(ctx, mcVersion, neoforgeVersion)

	index := MrpackIndex{
		FormatVersion: 1,
		Game:          "minecraft",
		VersionID:     time.Now().UTC().Format("2006.01.02"),
		Name:          packName,
		Summary:       fmt.Sprintf("Client modpack for %s on Minecraft %s (NeoForge %s)", packName, mcVersion, resolvedNF),
		Dependencies: map[string]string{
			"minecraft": mcVersion,
			"neoforge":  resolvedNF,
		},
		Files: []MrpackFile{},
	}

	for _, slug := range slugs {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}

		proj, err := mr.GetProject(ctx, slug)
		if err != nil {
			slog.Warn("mrpack: skipping mod, could not fetch project metadata", "slug", slug, "err", err)
			continue
		}

		// Filter out server-side only mods
		if strings.EqualFold(proj.ClientSide, "unsupported") {
			slog.Info("mrpack: excluding server-only mod from client pack", "slug", slug)
			continue
		}

		versions, err := mr.GetProjectVersions(ctx, slug, mcVersion)
		if err != nil || len(versions) == 0 {
			slog.Warn("mrpack: skipping mod, no compatible versions found", "slug", slug, "mcVersion", mcVersion)
			continue
		}

		// Choose latest version
		ver := versions[0]
		var chosenFile *modrinth.VersionFile
		for _, f := range ver.Files {
			if f.Primary && strings.HasSuffix(f.FileName, ".jar") {
				copyF := f
				chosenFile = &copyF
				break
			}
		}
		if chosenFile == nil {
			for _, f := range ver.Files {
				if strings.HasSuffix(f.FileName, ".jar") {
					copyF := f
					chosenFile = &copyF
					break
				}
			}
		}

		if chosenFile == nil || chosenFile.URL == "" {
			slog.Warn("mrpack: no valid jar file found in version", "slug", slug, "version", ver.VersionNum)
			continue
		}

		clientEnv := "required"
		if strings.EqualFold(proj.ClientSide, "optional") {
			clientEnv = "optional"
		}
		serverEnv := "required"
		if proj.ServerSide != "" {
			serverEnv = strings.ToLower(proj.ServerSide)
		}

		hashes := make(map[string]string)
		for k, v := range chosenFile.Hashes {
			hashes[k] = v
		}

		index.Files = append(index.Files, MrpackFile{
			Path:   "mods/" + chosenFile.FileName,
			Hashes: hashes,
			Env: &MrpackEnv{
				Client: clientEnv,
				Server: serverEnv,
			},
			Downloads: []string{chosenFile.URL},
			FileSize:  chosenFile.Size,
		})
	}

	indexJSON, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal modrinth.index.json: %w", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// 1. Write modrinth.index.json
	iw, err := zw.Create("modrinth.index.json")
	if err != nil {
		return nil, err
	}
	if _, err := iw.Write(indexJSON); err != nil {
		return nil, err
	}

	// 2. Write configs into overrides/config/
	var configNames []string
	for k := range configs {
		configNames = append(configNames, k)
	}
	sort.Strings(configNames)

	for _, name := range configNames {
		cw, err := zw.Create("overrides/config/" + name)
		if err != nil {
			return nil, err
		}
		if _, err := cw.Write([]byte(configs[name])); err != nil {
			return nil, err
		}
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
