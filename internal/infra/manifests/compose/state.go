package compose

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"

	"agrelha/internal/domain"
)

const (
	ModsFileName    = "mods.yaml"
	ConfigsFileName = "configs.yaml"
)

type stateDoc struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   stateMeta         `yaml:"metadata"`
	Data       map[string]string `yaml:"data"`
}

type stateMeta struct {
	Name   string            `yaml:"name"`
	Labels map[string]string `yaml:"labels,omitempty"`
}

func RenderStateFiles(profile domain.GameProfile, inst domain.Instance, mods domain.ModList) (map[string][]byte, error) {
	files := map[string][]byte{}
	labels := map[string]string{
		"app":      inst.DeploymentName(profile),
		"instance": fmt.Sprintf("%d", inst.Number),
	}

	if profile.Capabilities.Mods {
		data := map[string]string{"mods.txt": mods.Primary}
		if mods.CurseForge != "" {
			data["curseforge.txt"] = mods.CurseForge
		}
		body, err := encodeState(inst.ModsCMName(profile), labels, data)
		if err != nil {
			return nil, err
		}
		files[ModsFileName] = body
	}

	if profile.Capabilities.Configs {
		body, err := encodeState(inst.ConfigsCMName(profile), labels, map[string]string{})
		if err != nil {
			return nil, err
		}
		files[ConfigsFileName] = body
	}
	return files, nil
}

func encodeState(name string, labels, data map[string]string) ([]byte, error) {
	doc := stateDoc{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Metadata:   stateMeta{Name: name, Labels: labels},
		Data:       data,
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode %s: %w", name, err)
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
