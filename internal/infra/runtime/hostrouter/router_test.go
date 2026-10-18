package hostrouter

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type spyRuntime struct {
	name    string
	started []string
}

func (s *spyRuntime) Start(_ context.Context, ref ports.ServerRef) error {
	s.started = append(s.started, ref.Name)
	return nil
}
func (s *spyRuntime) Stop(context.Context, ports.ServerRef) error    { return nil }
func (s *spyRuntime) Restart(context.Context, ports.ServerRef) error { return nil }
func (s *spyRuntime) Status(context.Context, ports.ServerRef) (ports.Status, error) {
	return ports.Status{Available: true}, nil
}
func (s *spyRuntime) Metrics(context.Context, ports.ServerRef) (ports.Metrics, error) {
	return ports.Metrics{Known: true}, nil
}
func (s *spyRuntime) Logs(context.Context, ports.ServerRef, ports.LogOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(s.name)), nil
}
func (s *spyRuntime) WatchAvailability(context.Context, ports.ServerRef, time.Duration) error {
	return nil
}

func TestEmptyHostRoutesToCluster(t *testing.T) {
	cluster := &spyRuntime{name: "cluster"}
	box := &spyRuntime{name: "fogo"}
	r := New(map[domain.Host]ports.Runtime{domain.HostCluster: cluster, "fogo": box})

	if err := r.Start(context.Background(), ports.ServerRef{Name: "valheim-odin-01"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(cluster.started) != 1 || cluster.started[0] != "valheim-odin-01" {
		t.Errorf("an unset Host must route to the cluster, got %v", cluster.started)
	}
	if len(box.started) != 0 {
		t.Error("the box runtime must not have been touched")
	}
}

func TestExplicitHostRoutesToItsRuntime(t *testing.T) {
	cluster := &spyRuntime{name: "cluster"}
	box := &spyRuntime{name: "fogo"}
	r := New(map[domain.Host]ports.Runtime{domain.HostCluster: cluster, "fogo": box})

	if err := r.Start(context.Background(), ports.ServerRef{Name: "valheim-fogo-01", Host: "fogo"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(box.started) != 1 {
		t.Errorf("expected the fogo runtime to start it, got %v", box.started)
	}
	if len(cluster.started) != 0 {
		t.Error("a fogo world must never be started against the cluster")
	}
}

func TestUnknownHostIsAnErrorNotAFallback(t *testing.T) {
	cluster := &spyRuntime{name: "cluster"}
	r := New(map[domain.Host]ports.Runtime{domain.HostCluster: cluster})

	err := r.Start(context.Background(), ports.ServerRef{Name: "valheim-ghost-01", Host: "nowhere"})
	if err == nil {
		t.Fatal("an unregistered Host must be a hard error, never a silent fallback")
	}
	if !strings.Contains(err.Error(), "nowhere") {
		t.Errorf("error should name the host, got %v", err)
	}
	if len(cluster.started) != 0 {
		t.Error("nothing may be started when the Host is unknown")
	}
}

func TestEveryMethodRefusesAnUnknownHost(t *testing.T) {
	r := New(map[domain.Host]ports.Runtime{domain.HostCluster: &spyRuntime{}})
	ref := ports.ServerRef{Name: "x", Host: "nowhere"}
	ctx := context.Background()

	checks := map[string]error{
		"Stop":    r.Stop(ctx, ref),
		"Restart": r.Restart(ctx, ref),
	}
	if _, err := r.Status(ctx, ref); err != nil {
		checks["Status"] = err
	} else {
		checks["Status"] = nil
	}
	if _, err := r.Metrics(ctx, ref); err != nil {
		checks["Metrics"] = err
	} else {
		checks["Metrics"] = nil
	}
	if _, err := r.Logs(ctx, ref, ports.LogOptions{}); err != nil {
		checks["Logs"] = err
	} else {
		checks["Logs"] = nil
	}
	checks["WatchAvailability"] = r.WatchAvailability(ctx, ref, time.Second)

	for name, err := range checks {
		if err == nil {
			t.Errorf("%s accepted an unknown Host", name)
		}
	}
}

func TestNilRuntimesAreNotRegistered(t *testing.T) {
	r := New(map[domain.Host]ports.Runtime{domain.HostCluster: &spyRuntime{}, "fogo": nil})

	if _, err := r.For("fogo"); err == nil {
		t.Error("a nil runtime must not register as a usable Host")
	}
	if _, err := r.For(domain.HostCluster); err != nil {
		t.Errorf("cluster should still resolve: %v", err)
	}
}

func TestHostIsCaseAndSpaceInsensitive(t *testing.T) {
	box := &spyRuntime{name: "fogo"}
	r := New(map[domain.Host]ports.Runtime{"FOGO": box})

	if _, err := r.For("  fogo  "); err != nil {
		t.Errorf("host lookup should normalize: %v", err)
	}
	if _, err := r.For("other"); err == nil {
		t.Error("an unrelated host must still fail")
	}
}
