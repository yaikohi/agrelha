package minecraft

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

func TestMinecraftGameImplementation(t *testing.T) {
	g := New()
	var _ ports.Game = g

	if g.ID() != domain.GameMinecraft {
		t.Errorf("ID = %s, want minecraft", g.ID())
	}

	display := g.Display()
	if display.Name != "Minecraft" || display.Icon != "pickaxe" || display.Accent != "emerald" {
		t.Errorf("Display unexpected: %+v", display)
	}

	if g.AdmissionModel() != domain.AdmissionAllowlist {
		t.Errorf("AdmissionModel = %s, want allowlist", g.AdmissionModel())
	}

	if g.OperatorIDKind() != domain.IDKindUsername {
		t.Errorf("OperatorIDKind = %s, want username", g.OperatorIDKind())
	}
}

func TestMinecraftRuntimeSpec(t *testing.T) {
	g := New(WithImage("custom/mc:java25"))
	inst := domain.Instance{
		Number:    1,
		Name:      "Fluxweave",
		Slug:      "fluxweave",
		Loader:    domain.LoaderNeoForge,
		Source:    domain.SourceModlist,
		MCVersion: "1.21.1",
	}
	spec := g.RuntimeSpec(inst)

	if spec.Image != "custom/mc:java25" {
		t.Errorf("Image = %s, want custom/mc:java25", spec.Image)
	}
	if len(spec.Ports) != 2 {
		t.Fatalf("Ports count = %d, want 2", len(spec.Ports))
	}
	if spec.Ports[0].Port != 25565 || spec.Ports[0].Protocol != "TCP" {
		t.Errorf("Port 0 unexpected: %+v", spec.Ports[0])
	}
	if spec.Ports[1].Port != 25575 || spec.Ports[1].Protocol != "TCP" {
		t.Errorf("Port 1 unexpected: %+v", spec.Ports[1])
	}
	if spec.Env["LEVEL"] != "fluxweave" {
		t.Errorf("LEVEL = %q, want fluxweave", spec.Env["LEVEL"])
	}
}

func TestMinecraftExportClientBundle(t *testing.T) {
	g := New()
	ctx := context.Background()
	inst := domain.Instance{
		Name:      "Ayyy World",
		Slug:      "ayyy-world",
		Loader:    domain.LoaderNeoForge,
		MCVersion: "1.21.1",
	}

	bundle, err := g.ExportClientBundle(ctx, inst)
	if err != nil {
		t.Fatalf("ExportClientBundle failed: %v", err)
	}
	if bundle.Filename != "ayyy-world.mrpack" {
		t.Errorf("Filename = %q, want ayyy-world.mrpack", bundle.Filename)
	}
	if bundle.ContentType != "application/x-modrinth-modpack+zip" {
		t.Errorf("ContentType = %q", bundle.ContentType)
	}

	// Read zip contents
	zr, err := zip.NewReader(bytes.NewReader(bundle.Data), int64(len(bundle.Data)))
	if err != nil {
		t.Fatalf("unzip mrpack bundle: %v", err)
	}
	foundIndex := false
	for _, f := range zr.File {
		if f.Name == "modrinth.index.json" {
			foundIndex = true
			break
		}
	}
	if !foundIndex {
		t.Errorf("expected modrinth.index.json in .mrpack bundle")
	}
}

type fakeRuntime struct {
	status  ports.Status
	metrics ports.Metrics
}

func (f *fakeRuntime) Start(ctx context.Context, ref ports.ServerRef) error   { return nil }
func (f *fakeRuntime) Stop(ctx context.Context, ref ports.ServerRef) error    { return nil }
func (f *fakeRuntime) Restart(ctx context.Context, ref ports.ServerRef) error { return nil }
func (f *fakeRuntime) Status(ctx context.Context, ref ports.ServerRef) (ports.Status, error) {
	return f.status, nil
}
func (f *fakeRuntime) Metrics(ctx context.Context, ref ports.ServerRef) (ports.Metrics, error) {
	return f.metrics, nil
}
func (f *fakeRuntime) Logs(ctx context.Context, ref ports.ServerRef, opts ports.LogOptions) (io.ReadCloser, error) {
	return nil, nil
}

func (f *fakeRuntime) WatchAvailability(ctx context.Context, ref ports.ServerRef, timeout time.Duration) error {
	return nil
}

var _ ports.Runtime = (*fakeRuntime)(nil)

func TestMinecraftTelemetry(t *testing.T) {
	now := time.Now().Add(-45 * time.Minute)
	rt := &fakeRuntime{
		status: ports.Status{
			Lifecycle: ports.LifecycleRunning,
			Available: true,
			StartedAt: now,
		},
		metrics: ports.Metrics{
			CPUMillicores: 450,
			MemoryMiB:     4096,
			Known:         true,
		},
	}

	g := New(
		WithRuntime(rt, ports.ServerRef{Name: "minecraft", Scope: "minecraft-neoforge"}),
		WithPlayerCount(func(ctx context.Context) (int, error) { return 3, nil }),
		WithActiveInstance(func(ctx context.Context) (domain.Loader, string) {
			return domain.LoaderFabric, "Better Adventures"
		}),
	)

	tele, err := g.Telemetry(context.Background())
	if err != nil {
		t.Fatalf("Telemetry failed: %v", err)
	}

	if tele.State != "Up" || !tele.Online {
		t.Errorf("State = %s, Online = %v, want Up/true", tele.State, tele.Online)
	}
	if tele.Players != 3 || !tele.PlayersKnown {
		t.Errorf("Players = %d, PlayersKnown = %v, want 3/true", tele.Players, tele.PlayersKnown)
	}
	if tele.Loader != "Fabric" {
		t.Errorf("Loader = %s, want Fabric", tele.Loader)
	}
	if tele.PackName != "Better Adventures" {
		t.Errorf("PackName = %s, want Better Adventures", tele.PackName)
	}
	if tele.CPU != "450m" {
		t.Errorf("CPU = %s, want 450m", tele.CPU)
	}
	if tele.Memory != "4096 Mi" {
		t.Errorf("Memory = %s, want 4096 Mi", tele.Memory)
	}
	if !strings.Contains(tele.Uptime, "45m") {
		t.Errorf("Uptime = %s, want ~45m", tele.Uptime)
	}
}

func TestMinecraftBundleBuilder(t *testing.T) {
	g := New(
		WithBundleBuilder(func(ctx context.Context, inst domain.Instance) (domain.Bundle, error) {
			return domain.Bundle{
				Filename:    inst.Slug + "-custom.mrpack",
				ContentType: "application/x-modrinth-modpack+zip",
				Data:        []byte("fake-mrpack-bytes"),
			}, nil
		}),
	)

	b, err := g.ExportClientBundle(context.Background(), domain.Instance{Slug: "world-1"})
	if err != nil {
		t.Fatalf("ExportClientBundle failed: %v", err)
	}
	if b.Filename != "world-1-custom.mrpack" {
		t.Errorf("Filename = %q, want world-1-custom.mrpack", b.Filename)
	}
	if string(b.Data) != "fake-mrpack-bytes" {
		t.Errorf("Data unexpected: %s", string(b.Data))
	}
}
