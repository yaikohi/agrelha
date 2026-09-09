package modpack

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"agrelha/internal/infra/content/modrinth"
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
		client := &http.Client{Timeout: 10 * time.Second}
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
	case "1.20.2":
		return "20.2.88"
	case "1.20.1", "1.20":
		return "47.1.106"
	default:
		if strings.HasPrefix(mcVersion, "1.20.") {
			return "47.1.106"
		}
		return "21.1.249"
	}
}

// ModrinthProvider defines the subset of Modrinth client needed for building modpacks.
type ModrinthProvider interface {
	GetProject(ctx context.Context, idOrSlug string) (*modrinth.Project, error)
	GetProjectVersions(ctx context.Context, idOrSlug, mcVersion, loader string) ([]modrinth.Version, error)
}

// ModrinthBatchProvider optionally provides batch project resolution.
type ModrinthBatchProvider interface {
	GetProjects(ctx context.Context, idsOrSlugs []string) ([]modrinth.Project, error)
}

// BuildMrpack generates a Modrinth modpack (.mrpack) ZIP archive suitable for 1-click import into Prism Launcher.
// It filters out server-only mods (client_side == "unsupported"), resolves client files and hashes,
// packages declarative mod configs into overrides/config/, and writes an export report.
func BuildMrpack(ctx context.Context, mr ModrinthProvider, packName, mcVersion, loaderType, loaderVersion string, slugs []string, configs map[string]string) ([]byte, error) {
	if mcVersion == "" {
		mcVersion = "1.21.1"
	}
	loaderType = strings.ToLower(strings.TrimSpace(loaderType))
	if loaderType == "" {
		loaderType = "neoforge"
	}

	depMap := map[string]string{
		"minecraft": mcVersion,
	}
	var summary string
	if loaderType == "fabric" {
		fabricVer := loaderVersion
		if fabricVer == "" || fabricVer == "latest" {
			fabricVer = "0.16.10"
		}
		depMap["fabric-loader"] = fabricVer
		summary = fmt.Sprintf("Client modpack for %s on Minecraft %s (Fabric %s)", packName, mcVersion, fabricVer)
	} else {
		resolvedNF := ResolveNeoForgeVersion(ctx, mcVersion, loaderVersion)
		depMap["neoforge"] = resolvedNF
		summary = fmt.Sprintf("Client modpack for %s on Minecraft %s (NeoForge %s)", packName, mcVersion, resolvedNF)
	}

	index := MrpackIndex{
		FormatVersion: 1,
		Game:          "minecraft",
		VersionID:     time.Now().UTC().Format("2006.01.02"),
		Name:          packName,
		Summary:       summary,
		Dependencies:  depMap,
		Files:         []MrpackFile{},
	}

	// 1. Clean and deduplicate slugs
	var cleanSlugs []string
	seen := make(map[string]bool)
	for _, s := range slugs {
		s = strings.TrimSpace(s)
		s = strings.TrimSuffix(s, "?")
		if s == "" || strings.HasPrefix(s, "#") || seen[s] {
			continue
		}
		seen[s] = true
		cleanSlugs = append(cleanSlugs, s)
	}

	// 2. Fetch project metadata (using batch provider if available)
	projectMap := make(map[string]*modrinth.Project)
	if batchProvider, ok := mr.(ModrinthBatchProvider); ok {
		projects, err := batchProvider.GetProjects(ctx, cleanSlugs)
		if err == nil {
			for i := range projects {
				p := projects[i]
				projectMap[p.Slug] = &p
				projectMap[p.ID] = &p
			}
		} else {
			slog.Warn("mrpack: batch get projects failed, falling back to individual lookups", "err", err)
		}
	}

	var (
		serverOnlyMods []string
		missingMods    []string
		clientMods     []*modrinth.Project
	)

	for _, slug := range cleanSlugs {
		proj, ok := projectMap[slug]
		if !ok || proj == nil {
			p, err := mr.GetProject(ctx, slug)
			if err != nil {
				slog.Warn("mrpack: skipping mod, could not fetch project metadata", "slug", slug, "err", err)
				missingMods = append(missingMods, slug)
				continue
			}
			proj = p
			projectMap[slug] = p
		}

		// Filter out server-side only mods
		if strings.EqualFold(proj.ClientSide, "unsupported") {
			slog.Info("mrpack: excluding server-only mod from client pack", "slug", slug)
			serverOnlyMods = append(serverOnlyMods, slug)
			continue
		}

		clientMods = append(clientMods, proj)
	}

	// 3. Concurrently resolve version files for client mods
	type resolvedModFile struct {
		slug       string
		versionNum string
		file       MrpackFile
	}

	var (
		filesMu       sync.Mutex
		resolvedFiles []resolvedModFile
		noVersionMods []string
		wg            sync.WaitGroup
		sem           = make(chan struct{}, 6)
	)

	for _, proj := range clientMods {
		wg.Add(1)
		go func(p *modrinth.Project) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			versions, err := mr.GetProjectVersions(ctx, p.Slug, mcVersion, loaderType)
			if err != nil || len(versions) == 0 {
				slog.Warn("mrpack: skipping mod, no compatible versions found", "slug", p.Slug, "mcVersion", mcVersion, "loader", loaderType)
				filesMu.Lock()
				noVersionMods = append(noVersionMods, p.Slug)
				filesMu.Unlock()
				return
			}

			// Choose best version: prefer primary version file with .jar extension
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
				slog.Warn("mrpack: no valid jar file found in version", "slug", p.Slug, "version", ver.VersionNum)
				filesMu.Lock()
				noVersionMods = append(noVersionMods, p.Slug)
				filesMu.Unlock()
				return
			}

			clientEnv := "required"
			if strings.EqualFold(p.ClientSide, "optional") {
				clientEnv = "optional"
			}
			serverEnv := "required"
			if p.ServerSide != "" {
				serverEnv = strings.ToLower(p.ServerSide)
			}

			hashes := make(map[string]string)
			maps.Copy(hashes, chosenFile.Hashes)

			filesMu.Lock()
			resolvedFiles = append(resolvedFiles, resolvedModFile{
				slug:       p.Slug,
				versionNum: ver.VersionNum,
				file: MrpackFile{
					Path:   "mods/" + chosenFile.FileName,
					Hashes: hashes,
					Env: &MrpackEnv{
						Client: clientEnv,
						Server: serverEnv,
					},
					Downloads: []string{chosenFile.URL},
					FileSize:  chosenFile.Size,
				},
			})
			filesMu.Unlock()
		}(proj)
	}

	wg.Wait()

	// Sort files deterministically by file path
	sort.Slice(resolvedFiles, func(i, j int) bool {
		return resolvedFiles[i].file.Path < resolvedFiles[j].file.Path
	})
	for _, rf := range resolvedFiles {
		index.Files = append(index.Files, rf.file)
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

	// 3. Write diagnostic export report into overrides/MOD_EXPORT_REPORT.txt
	var report strings.Builder
	report.WriteString("============================================================\n")
	report.WriteString("Modpack Export Report\n")
	report.WriteString(fmt.Sprintf("Name:              %s\n", packName))
	report.WriteString(fmt.Sprintf("Minecraft Version: %s\n", mcVersion))
	report.WriteString(fmt.Sprintf("Loader:            %s (%s)\n", loaderType, depMap[loaderType]))
	report.WriteString(fmt.Sprintf("Export Date:       %s\n", time.Now().UTC().Format(time.RFC3339)))
	report.WriteString("============================================================\n")
	report.WriteString(fmt.Sprintf("Total Server Mods: %d\n", len(cleanSlugs)))
	report.WriteString(fmt.Sprintf("Included Client:   %d\n", len(index.Files)))
	report.WriteString(fmt.Sprintf("Server-Only:       %d\n", len(serverOnlyMods)))
	report.WriteString(fmt.Sprintf("Missing/Unfound:   %d\n", len(missingMods)))
	report.WriteString(fmt.Sprintf("No Version Found:  %d\n", len(noVersionMods)))
	report.WriteString("============================================================\n\n")

	report.WriteString("[INCLUDED CLIENT MODS]\n")
	for _, rf := range resolvedFiles {
		report.WriteString(fmt.Sprintf("+ %s (%s) -> %s\n", rf.slug, rf.versionNum, rf.file.Path))
	}
	report.WriteString("\n")

	if len(serverOnlyMods) > 0 {
		sort.Strings(serverOnlyMods)
		report.WriteString("[SERVER-ONLY MODS (OMITTED SAFELY)]\n")
		for _, s := range serverOnlyMods {
			report.WriteString(fmt.Sprintf("- %s\n", s))
		}
		report.WriteString("\n")
	}

	if len(missingMods) > 0 {
		sort.Strings(missingMods)
		report.WriteString("[MODS NOT FOUND ON MODRINTH (CURSEFORGE EXCLUSIVE OR MANUAL)]\n")
		for _, s := range missingMods {
			report.WriteString(fmt.Sprintf("! %s\n", s))
		}
		report.WriteString("\n")
	}

	if len(noVersionMods) > 0 {
		sort.Strings(noVersionMods)
		report.WriteString("[MODS WITHOUT COMPATIBLE VERSION FOR THIS MC/LOADER]\n")
		for _, s := range noVersionMods {
			report.WriteString(fmt.Sprintf("? %s\n", s))
		}
		report.WriteString("\n")
	}

	rw, err := zw.Create("overrides/MOD_EXPORT_REPORT.txt")
	if err == nil {
		_, _ = rw.Write([]byte(report.String()))
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
