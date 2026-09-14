// Package minecraft provides mod and access management for the NeoForge Minecraft server.
package access

import (
	"context"
	"fmt"
	"strings"

	"agrelha/internal/ports"
)

const DefaultModsPath = "manifests/minecraft-modded/mods.yaml"

type ModManager struct {
	store     ports.StateStore
	path      string // relPath of mod configmap in yaya-ops
	loaderKey string // "NEOFORGE_VERSION" or "FABRIC_VERSION"
}

func NewModManager(store ports.StateStore, path string) *ModManager {
	if path == "" {
		path = DefaultModsPath
	}
	return &ModManager{store: store, path: path, loaderKey: "NEOFORGE_VERSION"}
}

// ParseMods parses the lines of mods.txt, filtering out comments and blank lines.
func ParseMods(content string) []string {
	var out []string
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// Install adds new mod slugs to mods.txt if not already present.
func (m *ModManager) Install(ctx context.Context, slugs []string) (bool, error) {
	if m.store == nil {
		return false, ports.ErrNotImplemented
	}
	msg := fmt.Sprintf("mc-mods: install %s", strings.Join(slugs, ", "))
	return m.store.Patch(ctx, m.path, msg, func(doc *ports.Document) (bool, error) {
		cur := ""
		if doc.Data != nil {
			cur = doc.Data["mods.txt"]
		}
		present := make(map[string]bool)
		for _, s := range ParseMods(cur) {
			present[s] = true
		}

		var body strings.Builder
		body.WriteString(strings.TrimRight(cur, "\n"))
		added := 0
		for _, s := range slugs {
			s = strings.TrimSpace(s)
			if s == "" || present[s] {
				continue
			}
			body.WriteString("\n" + s)
			present[s] = true
			added++
		}

		if added == 0 {
			return false, nil
		}
		if doc.Data == nil {
			doc.Data = make(map[string]string)
		}
		doc.Data["mods.txt"] = strings.TrimLeft(body.String(), "\n") + "\n"
		return true, nil
	})
}

// Uninstall removes a mod slug from mods.txt.
func (m *ModManager) Uninstall(ctx context.Context, slug string) (bool, error) {
	if m.store == nil {
		return false, ports.ErrNotImplemented
	}
	slug = strings.TrimSpace(slug)
	msg := fmt.Sprintf("mc-mods: remove %s", slug)
	return m.store.Patch(ctx, m.path, msg, func(doc *ports.Document) (bool, error) {
		cur := ""
		if doc.Data != nil {
			cur = doc.Data["mods.txt"]
		}
		var lines []string
		found := false
		for line := range strings.SplitSeq(cur, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == slug {
				found = true
				continue
			}
			lines = append(lines, line)
		}
		if !found {
			return false, nil
		}
		doc.Data["mods.txt"] = strings.Join(lines, "\n")
		return true, nil
	})
}

// SetVersion updates MINECRAFT_VERSION and the loader version (NEOFORGE_VERSION or FABRIC_VERSION).
func (m *ModManager) SetVersion(ctx context.Context, mcVersion, loaderVersion string) (bool, error) {
	if m.store == nil {
		return false, ports.ErrNotImplemented
	}
	if loaderVersion == "" || loaderVersion == "recommended" {
		loaderVersion = "latest"
	}
	loaderKey := m.loaderKey
	if loaderKey == "" {
		loaderKey = "NEOFORGE_VERSION"
	}
	msg := fmt.Sprintf("mc-server: set MC=%s %s=%s", mcVersion, loaderKey, loaderVersion)
	return m.store.Patch(ctx, m.path, msg, func(doc *ports.Document) (bool, error) {
		changed := false
		if doc.Data == nil {
			doc.Data = make(map[string]string)
		}
		if mcVersion != "" && doc.Data["MINECRAFT_VERSION"] != strings.TrimSpace(mcVersion) {
			doc.Data["MINECRAFT_VERSION"] = strings.TrimSpace(mcVersion)
			changed = true
		}
		if loaderVersion != "" && doc.Data[loaderKey] != strings.TrimSpace(loaderVersion) {
			doc.Data[loaderKey] = strings.TrimSpace(loaderVersion)
			changed = true
		}
		return changed, nil
	})
}

// SwitchModpack replaces mods.txt with the mods from a modpack and optionally updates MINECRAFT_VERSION.
func (m *ModManager) SwitchModpack(ctx context.Context, packName, mcVersion string, slugs []string) (bool, error) {
	if m.store == nil {
		return false, ports.ErrNotImplemented
	}
	msg := fmt.Sprintf("mc-modpack: switch to %s (%d mods)", packName, len(slugs))
	return m.store.Patch(ctx, m.path, msg, func(doc *ports.Document) (bool, error) {
		if doc.Data == nil {
			doc.Data = make(map[string]string)
		}
		if mcVersion != "" {
			doc.Data["MINECRAFT_VERSION"] = strings.TrimSpace(mcVersion)
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("# Modpack: %s\n", packName))
		for _, s := range slugs {
			s = strings.TrimSpace(s)
			if s != "" {
				b.WriteString(s + "\n")
			}
		}
		doc.Data["mods.txt"] = b.String()
		return true, nil
	})
}
