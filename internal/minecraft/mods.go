// Package minecraft provides mod and access management for the NeoForge Minecraft server.
package minecraft

import (
	"context"
	"fmt"
	"strings"

	"agrelha/internal/gitops"
)

type ModManager struct {
	committer *gitops.Committer
	path      string // relPath of neoforge-mods.yaml in yaya-ops
}

func NewModManager(c *gitops.Committer, path string) *ModManager {
	if path == "" {
		path = "manifests/neoforge-mods.yaml"
	}
	return &ModManager{committer: c, path: path}
}

// ParseMods parses the lines of mods.txt, filtering out comments and blank lines.
func ParseMods(content string) []string {
	var out []string
	for _, line := range strings.Split(content, "\n") {
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
	msg := fmt.Sprintf("mc-mods: install %s", strings.Join(slugs, ", "))
	return m.committer.Patch(ctx, m.path, "mods.txt", msg, func(cur string) (string, error) {
		present := make(map[string]bool)
		for _, s := range ParseMods(cur) {
			present[s] = true
		}

		body := strings.TrimRight(cur, "\n")
		added := 0
		for _, s := range slugs {
			s = strings.TrimSpace(s)
			if s == "" || present[s] {
				continue
			}
			body += "\n" + s
			present[s] = true
			added++
		}

		if added == 0 {
			return cur, nil
		}
		return strings.TrimLeft(body, "\n") + "\n", nil
	})
}

// Uninstall removes a mod slug from mods.txt.
func (m *ModManager) Uninstall(ctx context.Context, slug string) (bool, error) {
	slug = strings.TrimSpace(slug)
	msg := fmt.Sprintf("mc-mods: remove %s", slug)
	return m.committer.Patch(ctx, m.path, "mods.txt", msg, func(cur string) (string, error) {
		var lines []string
		found := false
		for _, line := range strings.Split(cur, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == slug {
				found = true
				continue
			}
			lines = append(lines, line)
		}
		if !found {
			return cur, nil
		}
		return strings.Join(lines, "\n"), nil
	})
}

// SetVersion updates MINECRAFT_VERSION and NEOFORGE_VERSION in neoforge-mods.yaml.
func (m *ModManager) SetVersion(ctx context.Context, mcVersion, neoforgeVersion string) (bool, error) {
	msg := fmt.Sprintf("mc-server: set MC=%s NeoForge=%s", mcVersion, neoforgeVersion)
	changedMC, err := m.committer.Patch(ctx, m.path, "MINECRAFT_VERSION", msg, func(cur string) (string, error) {
		return strings.TrimSpace(mcVersion), nil
	})
	if err != nil {
		return false, err
	}

	if neoforgeVersion != "" {
		changedNF, err := m.committer.Patch(ctx, m.path, "NEOFORGE_VERSION", msg, func(cur string) (string, error) {
			return strings.TrimSpace(neoforgeVersion), nil
		})
		if err != nil {
			return changedMC, err
		}
		return changedMC || changedNF, nil
	}
	return changedMC, nil
}

// SwitchModpack replaces mods.txt with the mods from a modpack and optionally updates MINECRAFT_VERSION.
func (m *ModManager) SwitchModpack(ctx context.Context, packName, mcVersion string, slugs []string) (bool, error) {
	if mcVersion != "" {
		_, _ = m.SetVersion(ctx, mcVersion, "recommended")
	}

	msg := fmt.Sprintf("mc-modpack: switch to %s (%d mods)", packName, len(slugs))
	return m.committer.Patch(ctx, m.path, "mods.txt", msg, func(cur string) (string, error) {
		var b strings.Builder
		b.WriteString(fmt.Sprintf("# Modpack: %s\n", packName))
		for _, s := range slugs {
			s = strings.TrimSpace(s)
			if s != "" {
				b.WriteString(s + "\n")
			}
		}
		return b.String(), nil
	})
}
