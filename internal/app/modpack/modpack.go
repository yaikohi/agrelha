package modpack

import (
	"archive/zip"
	"bytes"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type versionNumber struct {
	Major int `yaml:"major"`
	Minor int `yaml:"minor"`
	Patch int `yaml:"patch"`
}

type exportMod struct {
	Name    string        `yaml:"name"`
	Version versionNumber `yaml:"version"`
	Enabled bool          `yaml:"enabled"`
}

type exportFormat struct {
	ProfileName string      `yaml:"profileName"`
	Mods        []exportMod `yaml:"mods"`
}

func Build(profileName string, entries []string, configs map[string]string) ([]byte, error) {
	ef := exportFormat{ProfileName: profileName}
	for _, e := range entries {
		if m, ok := parseEntry(e); ok {
			ef.Mods = append(ef.Mods, m)
		}
	}

	manifest, err := yaml.Marshal(ef)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	w, err := zw.Create("export.r2x")
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(manifest); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(configs))
	for name := range configs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cw, err := zw.Create("config/" + name)
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

func parseEntry(entry string) (exportMod, bool) {
	entry = strings.TrimSuffix(strings.TrimSpace(entry), "?")
	if entry == "" || strings.HasPrefix(entry, "#") {
		return exportMod{}, false
	}

	parts := strings.Split(entry, "/")
	if len(parts) < 3 {
		// agrelha stores mods as the Thunderstore full name "Namespace-Name",
		// with no pinned version. Dropping those is how an exported profile
		// ended up containing nothing but BepInEx.
		if ns, rest, ok := strings.Cut(entry, "-"); ok && ns != "" && rest != "" {
			// Namespace-Name-Version is Thunderstore's own identifier; the r2x
			// manifest wants the name and version separately.
			if name, version, ok := strings.Cut(rest, "-"); ok && looksLikeVersion(version) {
				return exportMod{Name: ns + "-" + name, Version: parseVersion(version), Enabled: true}, true
			}
			return exportMod{Name: entry, Enabled: true}, true
		}
		return exportMod{}, false
	}
	if parts[0] == "" || parts[1] == "" {
		return exportMod{}, false
	}
	v := parts[len(parts)-1]
	name := parts[len(parts)-2]
	ns := strings.Join(parts[:len(parts)-2], "/")
	return exportMod{
		Name:    ns + "-" + name,
		Version: parseVersion(v),
		Enabled: true,
	}, true
}

func parseVersion(v string) versionNumber {
	fields := strings.SplitN(v, ".", 3)
	get := func(i int) int {
		if i >= len(fields) {
			return 0
		}
		n, _ := strconv.Atoi(strings.TrimSpace(fields[i]))
		return n
	}
	return versionNumber{Major: get(0), Minor: get(1), Patch: get(2)}
}

// ResolveVersions turns bare Thunderstore full names ("Namespace-Name") into
// pinned "Namespace/Name/Version" entries using latest.
//
// r2modman resolves a profile entry by exact version, so an unversioned mod is
// reported as "not found on Thunderstore" and silently skipped on import.
// Entries that already carry a version, and names that cannot be resolved, are
// passed through untouched.
func ResolveVersions(entries []string, latest func(fullName string) (string, bool)) []string {
	if latest == nil {
		return entries
	}

	out := make([]string, 0, len(entries))
	for _, e := range entries {
		trimmed := strings.TrimSuffix(strings.TrimSpace(e), "?")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.Contains(trimmed, "/") {
			out = append(out, e)
			continue
		}
		ns, name, ok := strings.Cut(trimmed, "-")
		if !ok || ns == "" || name == "" {
			out = append(out, e)
			continue
		}
		if v, found := latest(trimmed); found && v != "" {
			out = append(out, ns+"/"+name+"/"+v)
			continue
		}
		out = append(out, e)
	}
	return out
}

func looksLikeVersion(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	return true
}
