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
	parts := strings.Split(strings.TrimSpace(entry), "/")
	if len(parts) < 3 || parts[0] == "" || parts[1] == "" {
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
