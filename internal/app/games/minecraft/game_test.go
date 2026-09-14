package minecraft

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"agrelha/internal/app/modpack"
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

type fakeMCRuntime struct {
	status ports.Status
}

func (f *fakeMCRuntime) Status(context.Context, ports.ServerRef) (ports.Status, error) {
	return f.status, nil
}
func (f *fakeMCRuntime) Metrics(context.Context, ports.ServerRef) (ports.Metrics, error) {
	return ports.Metrics{}, nil
}
func (f *fakeMCRuntime) Start(context.Context, ports.ServerRef) error    { return nil }
func (f *fakeMCRuntime) Stop(context.Context, ports.ServerRef) error     { return nil }
func (f *fakeMCRuntime) Restart(context.Context, ports.ServerRef) error  { return nil }
func (f *fakeMCRuntime) Logs(context.Context, ports.ServerRef, ports.LogOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (f *fakeMCRuntime) WatchAvailability(context.Context, ports.ServerRef, time.Duration) error {
	return nil
}

func TestMinecraftEdgeCasesAndOptions(t *testing.T) {
	ctx := context.Background()

	// 1. WithProvider & Providers()
	gProv := New(WithProvider(nil))
	if len(gProv.Providers()) != 1 {
		t.Errorf("expected 1 provider, got %d", len(gProv.Providers()))
	}

	// 2. WithStatusProvider
	gStat := New(WithStatusProvider(func(ctx context.Context) (domain.GameTelemetry, error) {
		return domain.GameTelemetry{State: "CustomMC"}, nil
	}))
	tele, err := gStat.Telemetry(ctx)
	if err != nil || tele.State != "CustomMC" {
		t.Errorf("unexpected status provider telemetry: %+v, err %v", tele, err)
	}

	// 3. WithContentResolver & ResolveContent
	gContent := New(WithContentResolver(func(ctx context.Context, inst domain.Instance) (domain.ContentSet, error) {
		return domain.ContentSet{Items: []domain.ContentItem{{Name: "ModItem"}}}, nil
	}))
	cs, err := gContent.ResolveContent(ctx, domain.Instance{})
	if err != nil || len(cs.Items) != 1 {
		t.Errorf("unexpected custom ResolveContent: %+v, err %v", cs, err)
	}
	// Default ResolveContent without resolver
	csDef, err := New().ResolveContent(ctx, domain.Instance{})
	if err != nil || len(csDef.Items) != 0 {
		t.Errorf("unexpected default ResolveContent: %+v, err %v", csDef, err)
	}

	// 4. activeInstanceFn with custom loader (e.g. Forge)
	gForge := New(WithActiveInstance(func(context.Context) (domain.Loader, string) {
		return domain.Loader("Forge"), "MyPack"
	}))
	teleForge, _ := gForge.Telemetry(ctx)
	if teleForge.Loader != "Forge" || teleForge.PackName != "MyPack" {
		t.Errorf("unexpected Forge loader telemetry: %+v", teleForge)
	}

	// 5. Telemetry with LifecycleStopped and custom Lifecycle
	rtStopped := &fakeMCRuntime{status: ports.Status{Lifecycle: ports.LifecycleStopped, Available: false}}
	gStopped := New(WithRuntime(rtStopped, ports.ServerRef{Name: "mc"}))
	teleStopped, _ := gStopped.Telemetry(ctx)
	if teleStopped.State != "Stopped" || teleStopped.Online {
		t.Errorf("expected Stopped telemetry, got %+v", teleStopped)
	}

	rtPending := &fakeMCRuntime{status: ports.Status{Lifecycle: ports.Lifecycle("CrashLoopBackOff"), Available: false}}
	gPending := New(WithRuntime(rtPending, ports.ServerRef{Name: "mc"}))
	telePending, _ := gPending.Telemetry(ctx)
	if telePending.State != "CrashLoopBackOff" {
		t.Errorf("expected CrashLoopBackOff telemetry, got %+v", telePending)
	}

	// 6. ExportClientBundle default without bundleBuilder (defaults for loader & mcVersion)
	gDefaultExport := New()
	b, err := gDefaultExport.ExportClientBundle(ctx, domain.Instance{Name: "DefaultWorld", Slug: "default-world"})
	if err != nil || b.Filename != "default-world.mrpack" {
		t.Errorf("unexpected default ExportClientBundle: %+v, err %v", b, err)
	}

	// 7. ExportClientBundle error when buildMrpack fails
	oldMrpack := buildMrpack
	buildMrpack = func(ctx context.Context, client modpack.ModrinthProvider, name, mcVersion, loader, loaderVersion string, mods []string, overrides map[string]string) ([]byte, error) {
		return nil, context.Canceled
	}
	defer func() { buildMrpack = oldMrpack }()

	if _, err := gDefaultExport.ExportClientBundle(ctx, domain.Instance{Name: "FailWorld", Slug: "fail-world"}); err == nil {
		t.Errorf("expected error in ExportClientBundle when buildMrpack fails")
	}
}

