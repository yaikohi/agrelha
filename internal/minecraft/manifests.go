package minecraft

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed templates/*
var templateFS embed.FS

type RenderData struct {
	Instance
	Annotations map[string]string
	Env         map[string]string
	ModsTxt     string
}

func (rd RenderData) IndentModsTxt() string {
	lines := strings.Split(strings.TrimRight(rd.ModsTxt, "\n"), "\n")
	var sb strings.Builder
	for _, l := range lines {
		sb.WriteString("    " + l + "\n")
	}
	return sb.String()
}

func RenderInstanceManifests(inst Instance, modsTxt string) (map[string][]byte, error) {
	inst.EnsureDefaults()

	data := RenderData{
		Instance:    inst,
		Annotations: inst.Annotations(),
		Env:         inst.Env(),
		ModsTxt:     modsTxt,
	}

	funcMap := template.FuncMap{
		"quote": func(s string) string {
			return fmt.Sprintf("%q", s)
		},
	}

	tmpl, err := template.New("manifests").Funcs(funcMap).ParseFS(templateFS, "templates/*.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	files := make(map[string][]byte)

	var depBuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&depBuf, "deployment.yaml.tmpl", data); err != nil {
		return nil, fmt.Errorf("render deployment: %w", err)
	}
	files["deployment.yaml"] = depBuf.Bytes()

	var svcBuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&svcBuf, "service.yaml.tmpl", data); err != nil {
		return nil, fmt.Errorf("render service: %w", err)
	}
	files["service.yaml"] = svcBuf.Bytes()

	var pvcBuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&pvcBuf, "pvc.yaml.tmpl", data); err != nil {
		return nil, fmt.Errorf("render pvc: %w", err)
	}
	files["pvc.yaml"] = pvcBuf.Bytes()

	var slotBuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&slotBuf, "slot.yaml.tmpl", data); err != nil {
		return nil, fmt.Errorf("render slot: %w", err)
	}
	files["slot.yaml"] = slotBuf.Bytes()

	var cfgBuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&cfgBuf, "configs.yaml.tmpl", data); err != nil {
		return nil, fmt.Errorf("render configs: %w", err)
	}
	files["configs.yaml"] = cfgBuf.Bytes()

	if inst.Source != SourceVanilla || strings.TrimSpace(modsTxt) != "" {
		if strings.TrimSpace(data.ModsTxt) == "" {
			data.ModsTxt = fmt.Sprintf("# Mod list for %s\n", inst.Name)
		}
		var modsBuf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&modsBuf, "mods.yaml.tmpl", data); err != nil {
			return nil, fmt.Errorf("render mods: %w", err)
		}
		files["mods.yaml"] = modsBuf.Bytes()
	}

	return files, nil
}
