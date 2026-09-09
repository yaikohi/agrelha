package minecraft

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"

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
