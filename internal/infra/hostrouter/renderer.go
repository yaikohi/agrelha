package hostrouter

import (
	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type RendererRouter struct {
	hostMap[ports.SpecRenderer]
}

func NewRenderer(routes map[domain.Host]ports.SpecRenderer) *RendererRouter {
	return &RendererRouter{hostMap: newHostMap("renderer", routes)}
}

var _ ports.SpecRenderer = (*RendererRouter)(nil)

func (r *RendererRouter) Render(inst domain.Instance, mods domain.ModList) (map[string][]byte, error) {
	rr, err := r.For(inst.EffectiveHost())
	if err != nil {
		return nil, err
	}
	return rr.Render(inst, mods)
}
