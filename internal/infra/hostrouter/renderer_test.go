package hostrouter

import (
	"strings"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type spyRenderer struct {
	name string
	seen []int
}

func (s *spyRenderer) Render(inst domain.Instance, _ domain.ModList) (map[string][]byte, error) {
	s.seen = append(s.seen, inst.Number)
	return map[string][]byte{s.name + ".yaml": []byte(s.name)}, nil
}

func TestRendererRoutesByInstanceHost(t *testing.T) {
	k8s := &spyRenderer{name: "deployment"}
	comp := &spyRenderer{name: "docker-compose"}
	r := NewRenderer(map[domain.Host]ports.SpecRenderer{domain.HostCluster: k8s, "fogo": comp})

	clusterWorld := domain.Instance{GameID: domain.GameValheim, Number: 1}
	files, err := r.Render(clusterWorld, domain.ModList{})
	if err != nil {
		t.Fatalf("render cluster: %v", err)
	}
	if _, ok := files["deployment.yaml"]; !ok {
		t.Errorf("a cluster world must render Kubernetes manifests, got %v", keys(files))
	}

	boxWorld := domain.Instance{GameID: domain.GameValheim, Number: 2, Host: "fogo"}
	files, err = r.Render(boxWorld, domain.ModList{})
	if err != nil {
		t.Fatalf("render fogo: %v", err)
	}
	if _, ok := files["docker-compose.yaml"]; !ok {
		t.Errorf("a fogo world must render compose, got %v", keys(files))
	}
	if len(k8s.seen) != 1 {
		t.Errorf("the k8s renderer saw %v, want only the cluster world", k8s.seen)
	}
}

func TestRendererRefusesUnknownHost(t *testing.T) {
	r := NewRenderer(map[domain.Host]ports.SpecRenderer{domain.HostCluster: &spyRenderer{name: "deployment"}})

	_, err := r.Render(domain.Instance{Number: 9, Host: "nowhere"}, domain.ModList{})
	if err == nil {
		t.Fatal("an unregistered Host must not fall back to another renderer")
	}
	if !strings.Contains(err.Error(), "nowhere") {
		t.Errorf("error should name the host, got %v", err)
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
