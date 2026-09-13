package valheim

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

func TestValheimGameImplementation(t *testing.T) {
	g := New()
	var _ ports.Game = g

	if g.ID() != domain.GameValheim {
		t.Errorf("ID = %s, want valheim", g.ID())
	}

	display := g.Display()
	if display.Name != "Valheim" || display.Icon != "axe" || display.Accent != "amber" {
		t.Errorf("Display unexpected: %+v", display)
	}

	if g.AdmissionModel() != domain.AdmissionPassword {
		t.Errorf("AdmissionModel = %s, want password", g.AdmissionModel())
	}

	if g.OperatorIDKind() != domain.IDKindSteam64 {
		t.Errorf("OperatorIDKind = %s, want steam64", g.OperatorIDKind())
	}
}

func TestValheimRuntimeSpec(t *testing.T) {
	g := New(WithImage("custom/valheim:1.0"))
	inst := domain.Instance{Name: "Odin's Hall", Slug: "odins-hall", MOTD: "Welcome"}
	spec := g.RuntimeSpec(inst)

	if spec.Image != "custom/valheim:1.0" {
		t.Errorf("Image = %s, want custom/valheim:1.0", spec.Image)
	}
	if len(spec.Ports) != 2 {
		t.Fatalf("Ports count = %d, want 2", len(spec.Ports))
	}
	if spec.Ports[0].Port != 2456 || spec.Ports[0].Protocol != "UDP" {
		t.Errorf("Port 0 unexpected: %+v", spec.Ports[0])
	}
	if spec.Ports[1].Port != 2457 || spec.Ports[1].Protocol != "UDP" {
		t.Errorf("Port 1 unexpected: %+v", spec.Ports[1])
	}
	if spec.Env["SERVER_NAME"] != "Odin's Hall" {
		t.Errorf("SERVER_NAME = %q", spec.Env["SERVER_NAME"])
	}
	if spec.Env["WORLD_NAME"] != "odins-hall" {
		t.Errorf("WORLD_NAME = %q", spec.Env["WORLD_NAME"])
	}
}

func TestWithBepInEx(t *testing.T) {
	t.Run("adds_fallback_version", func(t *testing.T) {
		entries := []string{"valheim-plus/valheim_plus/0.9.9"}
		res := WithBepInEx(entries, "")
		if len(res) != 2 {
			t.Fatalf("len = %d, want 2", len(res))
		}
		if res[0] != "denikson/BepInExPack_Valheim/"+FallbackBepInExVersion {
			t.Errorf("res[0] = %q", res[0])
		}
	})

	t.Run("adds_explicit_version", func(t *testing.T) {
		entries := []string{"valheim-plus/valheim_plus/0.9.9"}
		res := WithBepInEx(entries, "5.4.2400")
		if len(res) != 2 {
			t.Fatalf("len = %d, want 2", len(res))
		}
		if res[0] != "denikson/BepInExPack_Valheim/5.4.2400" {
			t.Errorf("res[0] = %q", res[0])
		}
	})

	t.Run("preserves_existing_bepinex", func(t *testing.T) {
		entries := []string{"custom/BepInExPack_Valheim/5.4.2100", "other/mod/1.0"}
		res := WithBepInEx(entries, "5.4.2400")
		if len(res) != 2 {
			t.Fatalf("len = %d, want 2", len(res))
		}
		if res[0] != "custom/BepInExPack_Valheim/5.4.2100" {
			t.Errorf("res[0] = %q, want existing BepInEx", res[0])
		}
	})
}

func TestValheimExportClientBundle(t *testing.T) {
	g := New()
	ctx := context.Background()
	bundle, err := g.ExportClientBundle(ctx, domain.Instance{Name: "Agrelha", Slug: "agrelha"})
	if err != nil {
		t.Fatalf("ExportClientBundle failed: %v", err)
	}
	if bundle.Filename != "agrelha-mods.r2z" {
		t.Errorf("Filename = %q, want agrelha-mods.r2z", bundle.Filename)
	}
	if bundle.ContentType != "application/zip" {
		t.Errorf("ContentType = %q", bundle.ContentType)
	}

	// Read zip contents
	zr, err := zip.NewReader(bytes.NewReader(bundle.Data), int64(len(bundle.Data)))
	if err != nil {
		t.Fatalf("unzip bundle: %v", err)
	}
	foundExportR2X := false
	for _, f := range zr.File {
		if f.Name == "export.r2x" {
			foundExportR2X = true
			break
		}
	}
	if !foundExportR2X {
		t.Errorf("expected export.r2x in .r2z bundle")
	}

	// BuildClientBundle with configs
	configs := map[string]string{"valheim_plus.cfg": "enabled=true"}
	b2, err := g.BuildClientBundle("Agrelha Server", []string{"author/mod/1.0.0"}, configs, "5.4.2333")
	if err != nil {
		t.Fatalf("BuildClientBundle failed: %v", err)
	}
	if !strings.HasSuffix(b2.Filename, ".r2z") {
		t.Errorf("expected .r2z filename, got %q", b2.Filename)
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

func TestValheimTelemetry(t *testing.T) {
	now := time.Now().Add(-2 * time.Hour)
	rt := &fakeRuntime{
		status: ports.Status{
			Lifecycle: ports.LifecycleRunning,
			Available: true,
			StartedAt: now,
		},
		metrics: ports.Metrics{
			CPUMillicores: 120,
			MemoryMiB:     1024,
			Known:         true,
		},
	}

	g := New(
		WithRuntime(rt, ports.ServerRef{Name: "valheim", Scope: "valheim"}),
		WithPlayerCount(func() (int, error) { return 4, nil }),
	)

	tele, err := g.Telemetry(context.Background())
	if err != nil {
		t.Fatalf("Telemetry failed: %v", err)
	}

	if tele.State != "Up" || !tele.Online {
		t.Errorf("State = %s, Online = %v, want Up/true", tele.State, tele.Online)
	}
	if tele.Players != 4 || !tele.PlayersKnown {
		t.Errorf("Players = %d, PlayersKnown = %v, want 4/true", tele.Players, tele.PlayersKnown)
	}
	if tele.CPU != "120m" {
		t.Errorf("CPU = %s, want 120m", tele.CPU)
	}
	if tele.Memory != "1024 Mi" {
		t.Errorf("Memory = %s, want 1024 Mi", tele.Memory)
	}
	if !strings.Contains(tele.Uptime, "2h") {
		t.Errorf("Uptime = %s, want ~2h", tele.Uptime)
	}
}

func TestValheimResolveContentAndBundleSource(t *testing.T) {
	g := New(
		WithBundleSource(func(ctx context.Context, _ domain.Instance) ([]string, map[string]string, error) {
			return []string{"author/coolmod/1.2.0"}, map[string]string{"coolmod.cfg": "active=true"}, nil
		}),
	)

	ctx := context.Background()
	content, err := g.ResolveContent(ctx, domain.Instance{Name: "Agrelha"})
	if err != nil {
		t.Fatalf("ResolveContent failed: %v", err)
	}
	if len(content.Items) != 1 || content.Items[0].Name != "coolmod" || content.Items[0].Version != "1.2.0" {
		t.Errorf("unexpected content items: %+v", content.Items)
	}

	bundle, err := g.ExportClientBundle(ctx, domain.Instance{Name: "Server", Slug: "server"})
	if err != nil {
		t.Fatalf("ExportClientBundle failed: %v", err)
	}
	if bundle.Filename != "server-mods.r2z" {
		t.Errorf("Filename = %q, want server-mods.r2z", bundle.Filename)
	}
}

func TestExportClientBundleUsesTheInstancesMods(t *testing.T) {
	var sawInstance domain.Instance
	g := New(WithBundleSource(func(_ context.Context, inst domain.Instance) ([]string, map[string]string, error) {
		sawInstance = inst
		return []string{"Neobotics-SlayerSkills", "Smoothbrain-Mining"}, map[string]string{"mining.cfg": "x=1"}, nil
	}))

	inst := domain.Instance{GameID: domain.GameValheim, Number: 2, Name: "boppo", Slug: "boppo"}
	bundle, err := g.ExportClientBundle(context.Background(), inst)
	if err != nil {
		t.Fatalf("ExportClientBundle: %v", err)
	}

	if sawInstance.Number != 2 || sawInstance.Slug != "boppo" {
		t.Fatalf("the bundle source must be told which instance to export, got %+v", sawInstance)
	}
	if bundle.Filename != "boppo-mods.r2z" {
		t.Errorf("filename = %q", bundle.Filename)
	}

	zr, err := zip.NewReader(bytes.NewReader(bundle.Data), int64(len(bundle.Data)))
	if err != nil {
		t.Fatalf("bundle is not a zip: %v", err)
	}
	var manifest string
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if f.Name == "export.r2x" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			manifest = string(b)
		}
	}
	if manifest == "" {
		t.Fatalf("no export.r2x in the profile, entries: %v", names)
	}
	if !slices.Contains(names, "config/mining.cfg") {
		t.Errorf("mod configs must ship with the profile, entries: %v", names)
	}
	for _, want := range []string{"SlayerSkills", "Mining", "BepInEx"} {
		if !strings.Contains(manifest, want) {
			t.Errorf("exported profile is missing %q — it shipped only BepInEx before this: %s", want, manifest)
		}
	}
}

func TestExportResolvesBepInExRatherThanHardcodingIt(t *testing.T) {
	g := New(
		WithBepInExVersion(func(context.Context) (string, error) { return "5.4.2350", nil }),
		WithBundleSource(func(context.Context, domain.Instance) ([]string, map[string]string, error) {
			return []string{"Neobotics/SlayerSkills/1.2.0"}, nil, nil
		}),
	)

	b, err := g.ExportClientBundle(context.Background(), domain.Instance{GameID: domain.GameValheim, Number: 2, Name: "boppo", Slug: "boppo"})
	if err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(b.Data), int64(len(b.Data)))
	var m string
	for _, f := range zr.File {
		if f.Name == "export.r2x" {
			rc, _ := f.Open()
			x, _ := io.ReadAll(rc)
			rc.Close()
			m = string(x)
		}
	}
	if !strings.Contains(m, "denikson-BepInExPack_Valheim") {
		t.Fatalf("BepInEx must be mod #1:\n%s", m)
	}
	if !strings.Contains(m, "patch: 2350") {
		t.Errorf("must ship the resolved version, not the constant — a pre-1.0 BepInEx never loads on 1.0:\n%s", m)
	}
}

func TestExportFallsBackWhenResolverFails(t *testing.T) {
	g := New(
		WithBepInExVersion(func(context.Context) (string, error) { return "", errors.New("offline") }),
		WithBundleSource(func(context.Context, domain.Instance) ([]string, map[string]string, error) {
			return nil, nil, nil
		}),
	)
	b, err := g.ExportClientBundle(context.Background(), domain.Instance{GameID: domain.GameValheim, Number: 1, Name: "x", Slug: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b.Data, []byte("r2x")) && len(b.Data) == 0 {
		t.Error("a failed lookup must still produce a profile")
	}
}

func TestRuntimeSpecEnvMatchesTheInstance(t *testing.T) {
	inst := domain.Instance{
		GameID: domain.GameValheim, Number: 1, Name: "lareira-V2", Slug: "lareira-v2",
		Source: domain.SourceVanilla, Seed: "piertje", Password: "hunter2",
	}
	spec := New().RuntimeSpec(inst)

	// One env source: whatever Kubernetes gets, Docker gets.
	for k, want := range inst.Env() {
		if got := spec.Env[k]; got != want {
			t.Errorf("RuntimeSpec.Env[%q] = %q, want %q — a second env map is how Docker silently loses password/seed/BEPINEX", k, got, want)
		}
	}
	if spec.Env["BEPINEX"] != "false" {
		t.Error("a vanilla world must reach the container without BepInEx")
	}
	if strings.Contains(spec.HealthProbe, "status.json") {
		t.Error("status.json returns an empty body on Valheim 1.0; the probe must be the port-bound check")
	}
}
