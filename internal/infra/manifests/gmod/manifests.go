package gmod

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

const Image = "ceifa/garrysmod@sha256:51ca6117b29e9b9b4312d880da3bf16a058ab518ecf4449329cc57a37d58a30a"

const DataMountPath = "/home/gmod/server/garrysmod/data"

type Data struct {
	domain.Instance
	Profile           domain.GameProfile
	Annotations       map[string]string
	Env               map[string]string
	NodeSelectorKey   string
	NodeSelectorValue string
	Namespace         string
	Image             string
	DataMountPath     string
	SteamKeySecret    string
}

func (rd Data) DeploymentName() string { return rd.Instance.DeploymentName(rd.Profile) }
func (rd Data) ServiceName() string    { return rd.Instance.ServiceName(rd.Profile) }
func (rd Data) PVCName() string        { return rd.Instance.PVCName(rd.Profile) }
func (rd Data) ConfigCMName() string   { return rd.Instance.ConfigCMName(rd.Profile) }
func (rd Data) MemoryGiB() int         { return rd.Instance.MemoryGiB(rd.Profile) }
func (rd Data) MemoryLimitGiB() int    { return rd.Instance.MemoryLimitGiB(rd.Profile) }
func (rd Data) CPURequestMilli() int   { return rd.Instance.CPURequestMilli(rd.Profile) }
func (rd Data) CPULimitMilli() int     { return rd.Instance.CPULimitMilli(rd.Profile) }

type Renderer struct {
	nodeSelector string
	namespace    string
}

func New(nodeSelector, namespace string) *Renderer {
	return &Renderer{nodeSelector: nodeSelector, namespace: namespace}
}

var _ ports.SpecRenderer = (*Renderer)(nil)

func (r *Renderer) Render(inst domain.Instance, _ domain.ModList) (map[string][]byte, error) {
	return Render(inst, r.nodeSelector, r.namespace)
}

func Render(inst domain.Instance, nodeSelector, namespace string) (map[string][]byte, error) {
	inst.EnsureDefaults(domain.GModProfile, "")

	data := Data{
		Instance:       inst,
		Profile:        domain.GModProfile,
		Annotations:    inst.Annotations(domain.GModProfile),
		Env:            inst.Env(domain.GModProfile),
		Namespace:      namespace,
		Image:          Image,
		DataMountPath:  DataMountPath,
		SteamKeySecret: "gmod-secret",
	}
	if data.Namespace == "" {
		data.Namespace = "gmod"
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
		return nil, fmt.Errorf("parse gmod templates: %w", err)
	}

	files := make(map[string][]byte)
	for name, out := range map[string]string{
		"deployment.yaml.tmpl": "deployment.yaml",
		"service.yaml.tmpl":    "service.yaml",
		"pvc.yaml.tmpl":        "pvc.yaml",
		"slot.yaml.tmpl":       "slot.yaml",
	} {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
			return nil, fmt.Errorf("render %s: %w", out, err)
		}
		files[out] = buf.Bytes()
	}

	return files, nil
}
