package valheim

import (
	"fmt"
	"path/filepath"
	"strings"

	"agrelha/internal/domain"
	composereconcile "agrelha/internal/infra/reconcile/compose"
	"agrelha/internal/ports"
	"gopkg.in/yaml.v3"
)

const (
	ModReconcileService = "mod-reconcile"
	ReconcilerImage     = "alpine:3.20"
	ComposeFileName     = "docker-compose.yml"
	ModsFileName        = "mods.txt"
)

type ComposeRenderer struct {
	image    string
	dataRoot string
}

func NewComposeRenderer(image, dataRoot string) *ComposeRenderer {
	if image == "" {
		image = ServerImage
	}
	return &ComposeRenderer{image: image, dataRoot: dataRoot}
}

var _ ports.SpecRenderer = (*ComposeRenderer)(nil)

func (r *ComposeRenderer) Render(inst domain.Instance, mods domain.ModList) (map[string][]byte, error) {
	if strings.TrimSpace(r.dataRoot) == "" {
		return nil, fmt.Errorf("valheim compose render for instance %02d: no data root", inst.Number)
	}
	inst.EnsureDefaults(domain.ValheimProfile, "")

	data := Data{Instance: inst, Profile: domain.ValheimProfile}
	dir := composereconcile.InstanceDataDir(r.dataRoot, inst.ID(), inst.DeploymentName(domain.ValheimProfile))
	name := inst.DeploymentName(domain.ValheimProfile)

	raw := inst.Env(domain.ValheimProfile)
	raw["STATUS_HTTP_PORT"] = "9001"
	raw["POST_BOOTSTRAP_HOOK"] = data.PostBootstrapHook()
	raw["PRE_SERVER_RUN_HOOK"] = data.PreServerRunHook()
	if !inst.IsVanilla() {
		raw["PRE_BEPINEX_CONFIG_HOOK"] = data.PreBepInExConfigHook()
	}
	env := make(map[string]string, len(raw))
	for k, v := range raw {
		env[k] = composereconcile.EscapeVars(v)
	}

	server := composereconcile.ComposeService{
		ContainerName: name,
		Image:         r.image,
		Restart:       "unless-stopped",
		Environment:   env,
		Ports: []string{
			"2456:2456/udp",
			"2457:2457/udp",
			"9001:9001/tcp",
		},
		Volumes: []string{
			filepath.Join(dir, "config") + ":/config",
			filepath.Join(dir, "server") + ":/opt/valheim",
		},
		Healthcheck: &composereconcile.ComposeHealth{
			Test:        []string{"CMD-SHELL", `ss -lun | grep -qE ':2456[[:space:]]'`},
			Interval:    "30s",
			Timeout:     "5s",
			Retries:     3,
			StartPeriod: composereconcile.DefaultStartPeriod,
		},
	}

	services := map[string]composereconcile.ComposeService{}

	if !inst.IsVanilla() {
		services[ModReconcileService] = composereconcile.ComposeService{
			ContainerName: name + "-" + ModReconcileService,
			Image:         ReconcilerImage,
			Restart:       "no",
			Command:       []string{"sh", "-c", composereconcile.EscapeVars(modReconcileScript)},
			Volumes: []string{
				filepath.Join(dir, "config") + ":/config",
				"./" + ModsFileName + ":/config-mods/" + ModsFileName + ":ro",
			},
		}
		server.DependsOn = map[string]composereconcile.Depends{
			ModReconcileService: {Condition: "service_completed_successfully"},
		}
	}

	services[name] = server

	body, err := yaml.Marshal(composereconcile.ComposeFile{Services: services})
	if err != nil {
		return nil, fmt.Errorf("encode valheim compose: %w", err)
	}

	files := map[string][]byte{ComposeFileName: body}
	if !inst.IsVanilla() {
		list := mods.Primary
		if strings.TrimSpace(list) == "" {
			list = fmt.Sprintf("# Mod list for %s\n", inst.Name)
		}
		files[ModsFileName] = []byte(list)
	}
	return files, nil
}
