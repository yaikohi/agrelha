package compose

import (
	"fmt"
	"path/filepath"

	"agrelha/internal/domain"
	composereconcile "agrelha/internal/infra/reconcile/compose"
	"agrelha/internal/ports"
)

const (
	ComposeFileName = "docker-compose.yml"

	dataVolume            = "data"
	customConfigsVolume   = "custom-configs"
	configSyncServiceName = "config-sync"
	configSyncImage       = "busybox:1.36"
)

type Renderer struct {
	profile  domain.GameProfile
	dataRoot string
	spec     func(domain.Instance) domain.RuntimeSpec
}

func New(profile domain.GameProfile, dataRoot string, spec func(domain.Instance) domain.RuntimeSpec) *Renderer {
	return &Renderer{profile: profile, dataRoot: dataRoot, spec: spec}
}

var _ ports.SpecRenderer = (*Renderer)(nil)

func (r *Renderer) Render(inst domain.Instance, mods domain.ModList) (map[string][]byte, error) {
	if r.spec == nil {
		return nil, fmt.Errorf("compose renderer for %q has no RuntimeSpec source", r.profile.ID)
	}
	if r.profile.Capabilities.Mods && r.profile.ModDelivery != domain.ModDeliveryImage {
		return nil, fmt.Errorf("compose rendering for %q is not implemented: its Mod list is delivered by agrelha (%q), which needs an install step that RuntimeSpec does not describe", r.profile.ID, r.profile.ModDelivery)
	}

	inst.EnsureDefaults(r.profile, "")
	name := inst.DeploymentName(r.profile)
	spec := r.spec(inst)

	hostPorts, err := domain.WithHostPorts(r.profile, inst, spec.Ports)
	if err != nil {
		return nil, fmt.Errorf("render compose for %s: %w", name, err)
	}
	spec.Ports = hostPorts

	dataDir := composereconcile.InstanceDataDir(r.dataRoot, domain.NewInstanceID(r.profile.ID, inst.Number), name)
	body, err := composereconcile.RenderComposeWith(name, dataDir, spec, configSyncService(dataDir, spec))
	if err != nil {
		return nil, fmt.Errorf("render compose for %s: %w", name, err)
	}

	files := map[string][]byte{ComposeFileName: body}
	state, err := RenderStateFiles(r.profile, inst, mods)
	if err != nil {
		return nil, fmt.Errorf("render state files for %s: %w", name, err)
	}
	for fname, content := range state {
		files[fname] = content
	}
	return files, nil
}

func configSyncService(dataDir string, spec domain.RuntimeSpec) map[string]composereconcile.ComposeService {
	var source, dataMount string
	for _, v := range spec.Volumes {
		switch v.Name {
		case customConfigsVolume:
			source = v.MountPath
		case dataVolume:
			dataMount = v.MountPath
		}
	}
	if source == "" || dataMount == "" {
		return nil
	}
	script := fmt.Sprintf("mkdir -p %[2]s/config; if [ -d %[1]s ] && [ \"$$(ls -A %[1]s 2>/dev/null)\" ]; then cp -Lf %[1]s/* %[2]s/config/; fi", source, dataMount)
	return map[string]composereconcile.ComposeService{
		configSyncServiceName: {
			Image:   configSyncImage,
			Command: []string{"sh", "-c", script},
			Volumes: []string{
				filepath.Join(dataDir, dataVolume) + ":" + dataMount,
				filepath.Join(dataDir, customConfigsVolume) + ":" + source + ":ro",
			},
		},
	}
}
