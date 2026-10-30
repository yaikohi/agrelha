package compose

import (
	"fmt"
	"maps"
	"path/filepath"

	"agrelha/internal/domain"
	composereconcile "agrelha/internal/infra/reconcile/compose"
	"agrelha/internal/ports"
	"strings"
)

const (
	ComposeFileName = "docker-compose.yml"

	dataVolume            = "data"
	customConfigsVolume   = "custom-configs"
	configSyncServiceName = "config-sync"
	dataPermsServiceName  = "data-perms"
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

	if spec.Env == nil {
		spec.Env = map[string]string{}
	}
	maps.Copy(spec.Env, inst.EnvWith(r.profile, mods))

	hostPorts, err := domain.WithHostPorts(r.profile, inst, spec.Ports)
	if err != nil {
		return nil, fmt.Errorf("render compose for %s: %w", name, err)
	}
	spec.Ports = hostPorts

	dataDir := composereconcile.InstanceDataDir(r.dataRoot, domain.NewInstanceID(r.profile.ID, inst.Number), name)
	extra := configSyncService(dataDir, spec)
	for n, svc := range dataPermsService(dataDir, spec) {
		if extra == nil {
			extra = map[string]composereconcile.ComposeService{}
		}
		extra[n] = svc
	}
	body, err := composereconcile.RenderComposeWith(name, dataDir, spec, extra)
	if err != nil {
		return nil, fmt.Errorf("render compose for %s: %w", name, err)
	}

	files := map[string][]byte{ComposeFileName: body}
	state, err := RenderStateFiles(r.profile, inst, mods)
	if err != nil {
		return nil, fmt.Errorf("render state files for %s: %w", name, err)
	}
	maps.Copy(files, state)
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

func dataPermsService(dataDir string, spec domain.RuntimeSpec) map[string]composereconcile.ComposeService {
	if spec.RunAsUser <= 0 || len(spec.Volumes) == 0 {
		return nil
	}
	owner := fmt.Sprintf("%d", spec.RunAsUser)

	vols := make([]string, 0, len(spec.Volumes))
	paths := make([]string, 0, len(spec.Volumes))
	for _, v := range spec.Volumes {
		mount := "/mnt/" + v.Name
		vols = append(vols, filepath.Join(dataDir, v.Name)+":"+mount)
		paths = append(paths, mount)
	}

	return map[string]composereconcile.ComposeService{
		dataPermsServiceName: {
			Image:   configSyncImage,
			Command: []string{"sh", "-c", "chown -R " + owner + " " + strings.Join(paths, " ") + " && chmod -R g+rwX " + strings.Join(paths, " ")},
			Volumes: vols,
		},
	}
}
