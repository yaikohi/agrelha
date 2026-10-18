package compose

import (
	"fmt"

	"agrelha/internal/domain"
	composereconcile "agrelha/internal/infra/reconcile/compose"
	"agrelha/internal/ports"
)

const ComposeFileName = "docker-compose.yml"

type Renderer struct {
	profile domain.GameProfile
	spec    func(domain.Instance) domain.RuntimeSpec
}

func New(profile domain.GameProfile, spec func(domain.Instance) domain.RuntimeSpec) *Renderer {
	return &Renderer{profile: profile, spec: spec}
}

var _ ports.SpecRenderer = (*Renderer)(nil)

func (r *Renderer) Render(inst domain.Instance, _ domain.ModList) (map[string][]byte, error) {
	if r.spec == nil {
		return nil, fmt.Errorf("compose renderer for %q has no RuntimeSpec source", r.profile.ID)
	}
	if r.profile.Capabilities.Mods {
		return nil, fmt.Errorf("compose rendering for %q is not implemented: a world with per-mod installs needs a reconciliation step that RuntimeSpec does not describe", r.profile.ID)
	}

	inst.EnsureDefaults(r.profile, "")
	body, err := composereconcile.RenderCompose(inst.DeploymentName(r.profile), r.spec(inst))
	if err != nil {
		return nil, fmt.Errorf("render compose for %s: %w", inst.DeploymentName(r.profile), err)
	}
	return map[string][]byte{ComposeFileName: body}, nil
}
