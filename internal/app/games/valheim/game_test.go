package valheim

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"

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
