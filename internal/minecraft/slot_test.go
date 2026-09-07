package minecraft

import "testing"

func TestCurseForgeSlotOmitsVersion(t *testing.T) {
	d := SlotSpec{
		Slot:      "Ducktopia Farlands",
		Type:      TypeCurseForge,
		CFPageURL: "https://www.curseforge.com/minecraft/modpacks/ducktopia-farlands",
	}.Data()

	if _, ok := d["VERSION"]; ok {
		t.Fatal("AUTO_CURSEFORGE slot must NOT set VERSION: it constrains pack file selection and breaks the install")
	}
	for _, k := range []string{"NEOFORGE_VERSION", "FABRIC_LOADER_VERSION", "MODRINTH_PROJECTS"} {
		if _, ok := d[k]; ok {
			t.Fatalf("AUTO_CURSEFORGE slot leaked %q", k)
		}
	}
	if d["CF_PAGE_URL"] == "" {
		t.Fatal("CF_PAGE_URL missing")
	}
	if d["WORLD_SLOT"] != "ducktopia-farlands" {
		t.Fatalf("WORLD_SLOT = %q, want ducktopia-farlands", d["WORLD_SLOT"])
	}
}

func TestLoaderSlotsOmitCurseForgeKeys(t *testing.T) {
	for _, tc := range []struct{ typ, verKey string }{
		{TypeNeoForge, "NEOFORGE_VERSION"},
		{TypeFabric, "FABRIC_LOADER_VERSION"},
	} {
		d := SlotSpec{Slot: "vanilla-plus", Type: tc.typ, MCVersion: "26.2"}.Data()
		if _, ok := d["CF_PAGE_URL"]; ok {
			t.Fatalf("%s slot leaked CF_PAGE_URL", tc.typ)
		}
		if d["VERSION"] != "26.2" {
			t.Fatalf("%s VERSION = %q, want 26.2", tc.typ, d["VERSION"])
		}
		if d[tc.verKey] != "latest" {
			t.Fatalf("%s %s = %q, want latest", tc.typ, tc.verKey, d[tc.verKey])
		}
		if d["MODRINTH_DOWNLOAD_DEPENDENCIES"] != "required" {
			t.Fatalf("%s must resolve required deps", tc.typ)
		}
	}
}

func TestSlotNameSanitises(t *testing.T) {
	cases := map[string]string{
		"Ducktopia Farlands":  "ducktopia-farlands",
		"ATM9: To the Sky!":   "atm9-to-the-sky",
		"  --Weird__Name--  ": "weird-name",
		"":                    "default",
		"!!!":                 "default",
	}
	for in, want := range cases {
		if got := SlotName(in); got != want {
			t.Fatalf("SlotName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetVersionRefusedOnCurseForgeSlot(t *testing.T) {
	m := NewSlotManager(nil, "", "")
	_, err := m.SetVersion(nil, SlotSpec{Slot: "ducktopia", Type: TypeCurseForge}, "26.2", "latest")
	if err == nil {
		t.Fatal("SetVersion must refuse on a CurseForge slot: the pack pins the MC version")
	}
}
