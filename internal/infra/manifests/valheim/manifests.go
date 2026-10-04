package valheim

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

//go:embed templates/*
var templateFS embed.FS

type Data struct {
	domain.Instance
	Profile           domain.GameProfile
	Annotations       map[string]string
	Env               map[string]string
	ModsTxt           string
	NodeSelectorKey   string
	NodeSelectorValue string
	Namespace         string
}

func (rd Data) DeploymentName() string { return rd.Instance.DeploymentName(rd.Profile) }
func (rd Data) ServiceName() string    { return rd.Instance.ServiceName(rd.Profile) }
func (rd Data) PVCName() string        { return rd.Instance.PVCName(rd.Profile) }
func (rd Data) ConfigCMName() string   { return rd.Instance.ConfigCMName(rd.Profile) }
func (rd Data) ModsCMName() string     { return rd.Instance.ModsCMName(rd.Profile) }
func (rd Data) ConfigsCMName() string  { return rd.Instance.ConfigsCMName(rd.Profile) }
func (rd Data) MemoryGiB() int         { return rd.Instance.MemoryGiB(rd.Profile) }
func (rd Data) MemoryLimitGiB() int    { return rd.Instance.MemoryLimitGiB(rd.Profile) }
func (rd Data) HeapInitMemoryGiB() int { return rd.Instance.HeapInitMemoryGiB(rd.Profile) }
func (rd Data) CPURequestMilli() int   { return rd.Instance.CPURequestMilli(rd.Profile) }
func (rd Data) CPULimitMilli() int     { return rd.Instance.CPULimitMilli(rd.Profile) }

func (rd Data) IndentModsTxt() string {
	lines := strings.Split(strings.TrimRight(rd.ModsTxt, "\n"), "\n")
	var sb strings.Builder
	for _, l := range lines {
		sb.WriteString("    " + l + "\n")
	}
	return sb.String()
}

// Renderer renders a Valheim Instance to Kubernetes manifests.
type Renderer struct {
	nodeSelector string
	namespace    string
}

func New(nodeSelector, namespace string) *Renderer {
	return &Renderer{nodeSelector: nodeSelector, namespace: namespace}
}

var _ ports.SpecRenderer = (*Renderer)(nil)

// Valheim has one catalogue (Thunderstore), so only the Primary list is ever
// populated; CurseForge is Minecraft's concern.
func (r *Renderer) Render(inst domain.Instance, mods domain.ModList) (map[string][]byte, error) {
	return Render(inst, mods.Primary, r.nodeSelector, r.namespace)
}

func Render(inst domain.Instance, modsTxt, nodeSelector, namespace string) (map[string][]byte, error) {
	inst.EnsureDefaults(domain.ValheimProfile, "")

	data := Data{
		Instance:    inst,
		Profile:     domain.ValheimProfile,
		Annotations: inst.Annotations(domain.ValheimProfile),
		Env:         inst.Env(domain.ValheimProfile),
		ModsTxt:     modsTxt,
		Namespace:   namespace,
	}
	if data.Namespace == "" {
		data.Namespace = "valheim"
	}
	if k, v, ok := strings.Cut(nodeSelector, "="); ok && strings.TrimSpace(k) != "" {
		data.NodeSelectorKey = strings.TrimSpace(k)
		data.NodeSelectorValue = strings.TrimSpace(v)
	}

	funcMap := template.FuncMap{
		"quote": func(s string) string {
			return fmt.Sprintf("%q", s)
		},
	}

	tmpl, err := template.New("manifests").Funcs(funcMap).ParseFS(templateFS, "templates/*.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parse valheim templates: %w", err)
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

	// The configs ConfigMap is rendered even for a Vanilla world, empty. Saving
	// an Override is a StateStore.Patch, and Patch cannot create a file that is
	// not there - while the only write that can, WriteDirectory, deletes every
	// sibling it was not given, which here is the rest of the instance's
	// manifests. So the file has to exist before anyone needs it, and a world
	// converted from Vanilla to Modded has no later chance to render one.
	// It costs an empty object; the deployment only mounts it when Modded.
	var cfgBuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&cfgBuf, "configs.yaml.tmpl", data); err != nil {
		return nil, fmt.Errorf("render configs: %w", err)
	}
	files["configs.yaml"] = cfgBuf.Bytes()

	// A Vanilla world has no Loader, so a mod list would be an object that
	// exists only to stay empty.
	if inst.IsVanilla() {
		return files, nil
	}

	if strings.TrimSpace(data.ModsTxt) == "" {
		data.ModsTxt = fmt.Sprintf("# Mod list for %s\n", inst.Name)
	}
	var modsBuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&modsBuf, "mods.yaml.tmpl", data); err != nil {
		return nil, fmt.Errorf("render mods: %w", err)
	}
	files["mods.yaml"] = modsBuf.Bytes()

	return files, nil
}
