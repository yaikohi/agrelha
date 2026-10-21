package wiring

import (
	"testing"

	"agrelha/internal/domain"
	hostrouter "agrelha/internal/infra/hostrouter"
	"agrelha/internal/infra/manifests"
	"agrelha/internal/platform/config"
)

func clusterWorld() domain.Instance {
	return domain.Instance{
		GameID: domain.GameMinecraft, Number: 1, Name: "Flux", Slug: "flux",
		Source:    domain.SourceModlist,
		Minecraft: &domain.MinecraftConfig{Loader: domain.LoaderNeoForge, MCVersion: "1.21.1"},
	}
}

func TestWithNoDockerHostClusterWorldsRenderAsBefore(t *testing.T) {
	cfg := &config.Config{}
	r := routeRenderer(cfg, manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded"), nil)

	files, err := r.Render(clusterWorld(), domain.ModList{})
	if err != nil {
		t.Fatalf("a cluster world must render exactly as before: %v", err)
	}
	if _, ok := files["deployment.yaml"]; !ok {
		t.Error("expected Kubernetes manifests for a cluster world")
	}
}

func TestAWorldOnAnUnconfiguredHostIsRefused(t *testing.T) {
	cfg := &config.Config{}
	r := routeRenderer(cfg, manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded"), nil)

	inst := clusterWorld()
	inst.Host = "fogo"
	if _, err := r.Render(inst, domain.ModList{}); err == nil {
		t.Error("a world declaring a Host with no configured renderer must fail, not silently render Kubernetes YAML into a Compose path")
	}
}

func TestNoRuntimeStaysNilWhenNothingIsConfigured(t *testing.T) {
	if rt := routeRuntime(&config.Config{}, nil); rt != nil {
		t.Error("with no cluster client and no Docker host there is no runtime; wrapping nil would turn a supported dev state into an error on every call")
	}
}

func TestDockerHostRegistersARuntime(t *testing.T) {
	cfg := &config.Config{DockerHostName: "fogo", DockerSocket: "/var/run/docker.sock"}
	rt := routeRuntime(cfg, nil)
	if rt == nil {
		t.Fatal("a configured Docker host must yield a runtime even with no cluster")
	}
	router, ok := rt.(*hostrouter.RuntimeRouter)
	if !ok {
		t.Fatalf("expected a host router, got %T", rt)
	}
	if _, err := router.For("fogo"); err != nil {
		t.Errorf("fogo is configured but not registered: %v", err)
	}
	if _, err := router.For(domain.HostCluster); err == nil {
		t.Error("with no cluster client, cluster must not resolve to a runtime")
	}
}

func TestHostNameIsNormalized(t *testing.T) {
	host, ok := dockerHost(&config.Config{DockerHostName: "  FOGO  "})
	if !ok || host != domain.Host("fogo") {
		t.Errorf("dockerHost = %q/%v, want fogo/true", host, ok)
	}
	if _, ok := dockerHost(&config.Config{}); ok {
		t.Error("no Docker host configured must report false")
	}
}
