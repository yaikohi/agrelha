package gmod

import (
	"context"
	"testing"

	"agrelha/internal/domain"
)

func TestGModGameRuntimeSpec(t *testing.T) {
	g := New(WithImage("custom/gmod:latest"))
	inst := domain.Instance{
		GameID: domain.GameGMod,
		Number: 1,
		Name:   "DarkRP",
		Slug:   "darkrp",
		Tier:   domain.TierMedium,
		GMod: &domain.GModConfig{
			Gamemode: "darkrp",
			Map:      "rp_downtown",
		},
	}

	spec := g.RuntimeSpec(inst)
	if spec.Image != "custom/gmod:latest" {
		t.Errorf("expected custom image, got %s", spec.Image)
	}
	if len(spec.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(spec.Ports))
	}
	if spec.Ports[0].Port != 27015 || spec.Ports[0].Protocol != "UDP" {
		t.Errorf("expected port 27015 UDP, got %v", spec.Ports[0])
	}
	if spec.Ports[1].Port != 27015 || spec.Ports[1].Protocol != "TCP" {
		t.Errorf("expected port 27015 TCP, got %v", spec.Ports[1])
	}
	if spec.HealthProbe != "/home/gmod/health.sh" {
		t.Errorf("expected health probe /home/gmod/health.sh, got %s", spec.HealthProbe)
	}
	if len(spec.Volumes) != 1 || spec.Volumes[0].MountPath != "/home/gmod/server/garrysmod/data" {
		t.Errorf("expected data volume mount, got %v", spec.Volumes)
	}
}

func TestGModGameTelemetry(t *testing.T) {
	ctx := context.Background()
	gDefault := New()
	tele, err := gDefault.Telemetry(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tele.PlayersKnown {
		t.Errorf("expected PlayersKnown to be false")
	}

	gCustom := New(WithStatusProvider(func(ctx context.Context) (domain.GameTelemetry, error) {
		return domain.GameTelemetry{
			Online: true,
			State:  "Up",
			CPU:    "100m",
		}, nil
	}))

	teleCustom, err := gCustom.Telemetry(ctx)
	if err != nil || !teleCustom.Online || teleCustom.State != "Up" {
		t.Errorf("unexpected custom telemetry: %+v, %v", teleCustom, err)
	}
}

func TestGModGameExportClientBundle(t *testing.T) {
	g := New()
	b, err := g.ExportClientBundle(context.Background(), domain.Instance{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Filename != "" || len(b.Data) != 0 {
		t.Errorf("expected empty bundle, got %+v", b)
	}
}
