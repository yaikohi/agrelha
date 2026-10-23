package hostrouter

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type hostMap[T any] struct {
	kind   string
	routes map[domain.Host]T
}

func newHostMap[T any](kind string, routes map[domain.Host]T) hostMap[T] {
	normalized := make(map[domain.Host]T, len(routes))
	for h, target := range routes {
		if isNil(target) {
			continue
		}
		normalized[domain.NormalizeHost(string(h))] = target
	}
	return hostMap[T]{kind: kind, routes: normalized}
}

func isNil[T any](v T) bool {
	if any(v) == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func (m hostMap[T]) Hosts() []string {
	out := make([]string, 0, len(m.routes))
	for h := range m.routes {
		out = append(out, string(h))
	}
	sort.Strings(out)
	return out
}

func (m hostMap[T]) For(host domain.Host) (T, error) {
	h := domain.NormalizeHost(string(host))
	target, ok := m.routes[h]
	if !ok {
		var zero T
		return zero, fmt.Errorf("no %s registered for host %q (known: %s): %w", m.kind, h, strings.Join(m.Hosts(), ", "), ports.ErrUnknownHost)
	}
	return target, nil
}
