package access

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"agrelha/internal/ports"
)

func TestParseMods(t *testing.T) {
	input := `
# Essential core mods
jei
ferrite-core

# Map & utility
journeymap
sophisticatedbackpacks:1.21.1-1.0
`
	expected := []string{
		"jei",
		"ferrite-core",
		"journeymap",
		"sophisticatedbackpacks:1.21.1-1.0",
	}

	res := ParseMods(input)
	if !reflect.DeepEqual(res, expected) {
		t.Fatalf("expected %v, got %v", expected, res)
	}
}

func TestParseUsers(t *testing.T) {
	input := `
# Server admins
ykhi
yaikohi
`
	expected := []string{"ykhi", "yaikohi"}
	res := ParseUsers(input)
	if !reflect.DeepEqual(res, expected) {
		t.Fatalf("expected %v, got %v", expected, res)
	}
}

func TestModManagerInstallAndUninstall(t *testing.T) {
	ctx := context.Background()
	ss := &mockStateStore{
		docs: map[string]ports.Document{
			DefaultModsPath: {
				Data: map[string]string{
					"mods.txt": "jei\njourneymap\n",
				},
			},
		},
	}

	mgr := NewModManager(ss, "")

	// 1. Install new mods
	changed, err := mgr.Install(ctx, []string{"ferrite-core", "jei", "sodium"})
	if err != nil || !changed {
		t.Fatalf("Install failed: changed=%v, err=%v", changed, err)
	}
	doc, _ := ss.Get(ctx, DefaultModsPath)
	mods := ParseMods(doc.Data["mods.txt"])
	expected := []string{"jei", "journeymap", "ferrite-core", "sodium"}
	if !reflect.DeepEqual(mods, expected) {
		t.Errorf("expected mods %v, got %v", expected, mods)
	}

	// 2. Install existing mods is no-op
	changed, err = mgr.Install(ctx, []string{"jei", "sodium"})
	if err != nil || changed {
		t.Fatalf("expected no-op for existing mods: changed=%v, err=%v", changed, err)
	}

	// 3. Uninstall mod
	changed, err = mgr.Uninstall(ctx, "journeymap")
	if err != nil || !changed {
		t.Fatalf("Uninstall failed: changed=%v, err=%v", changed, err)
	}
	doc, _ = ss.Get(ctx, DefaultModsPath)
	modsAfter := ParseMods(doc.Data["mods.txt"])
	if strings.Contains(strings.Join(modsAfter, " "), "journeymap") {
		t.Errorf("expected journeymap removed, got %v", modsAfter)
	}

	// 4. Uninstall non-existent mod
	changed, err = mgr.Uninstall(ctx, "nonexistent")
	if err != nil || changed {
		t.Fatalf("expected no-op for non-existent mod: changed=%v, err=%v", changed, err)
	}
}

func TestModManagerSetVersionAndSwitchModpack(t *testing.T) {
	ctx := context.Background()
	ss := &mockStateStore{
		docs: map[string]ports.Document{
			DefaultModsPath: {
				Data: map[string]string{
					"MINECRAFT_VERSION": "1.20.1",
					"NEOFORGE_VERSION":  "47.1.0",
				},
			},
		},
	}

	mgr := NewModManager(ss, "")

	// 1. SetVersion
	changed, err := mgr.SetVersion(ctx, "1.21.1", "21.1.65")
	if err != nil || !changed {
		t.Fatalf("SetVersion failed: changed=%v, err=%v", changed, err)
	}
	doc, _ := ss.Get(ctx, DefaultModsPath)
	if doc.Data["MINECRAFT_VERSION"] != "1.21.1" || doc.Data["NEOFORGE_VERSION"] != "21.1.65" {
		t.Errorf("unexpected versions: %+v", doc.Data)
	}

	// SetVersion recommended translates to latest
	changed, err = mgr.SetVersion(ctx, "", "recommended")
	if err != nil || !changed {
		t.Fatalf("SetVersion recommended failed: changed=%v, err=%v", changed, err)
	}
	doc, _ = ss.Get(ctx, DefaultModsPath)
	if doc.Data["NEOFORGE_VERSION"] != "latest" {
		t.Errorf("expected latest for recommended, got %s", doc.Data["NEOFORGE_VERSION"])
	}

	// 2. SwitchModpack
	changed, err = mgr.SwitchModpack(ctx, "All The Mods 9", "1.20.1", []string{"mod-a", "mod-b"})
	if err != nil || !changed {
		t.Fatalf("SwitchModpack failed: changed=%v, err=%v", changed, err)
	}
	doc, _ = ss.Get(ctx, DefaultModsPath)
	if doc.Data["MINECRAFT_VERSION"] != "1.20.1" {
		t.Errorf("expected 1.20.1, got %s", doc.Data["MINECRAFT_VERSION"])
	}
	mods := ParseMods(doc.Data["mods.txt"])
	if !reflect.DeepEqual(mods, []string{"mod-a", "mod-b"}) {
		t.Errorf("unexpected modpack mods: %v", mods)
	}
}
