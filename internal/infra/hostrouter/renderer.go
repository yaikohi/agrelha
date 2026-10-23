package hostrouter

import (
	"fmt"
	"sort"
	"strings"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type RendererRouter struct {
	routes map[domain.Host]ports.SpecRenderer
}

func NewRenderer(routes map[domain.Host]ports.SpecRenderer) *RendererRouter {
	normalized := make(map[domain.Host]ports.SpecRenderer, len(routes))
	for h, r := range routes {
		if r == nil {
			continue
		}
		normalized[domain.NormalizeHost(string(h))] = r
	}
	return &RendererRouter{routes: normalized}
}

var _ ports.SpecRenderer = (*RendererRouter)(nil)

func (r *RendererRouter) Hosts() []string {
	out := make([]string, 0, len(r.routes))
	for h := range r.routes {
		out = append(out, string(h))
	}
	sort.Strings(out)
	return out
}

func (r *RendererRouter) For(host domain.Host) (ports.SpecRenderer, error) {
	h := domain.NormalizeHost(string(host))
	rr, ok := r.routes[h]
	if !ok {
		return nil, fmt.Errorf("no renderer registered for host %q (known: %s): %w", h, strings.Join(r.Hosts(), ", "), ports.ErrUnknownHost)
	}
	return rr, nil
}

func (r *RendererRouter) Render(inst domain.Instance, mods domain.ModList) (map[string][]byte, error) {
	rr, err := r.For(inst.EffectiveHost())
	if err != nil {
		return nil, err
	}
	return rr.Render(inst, mods)
}
