package store

import (
	"path/filepath"
	"strings"
	"testing"

	"agrelha/internal/domain"
)

// Before the table refactor the repo was a binary switch that fell through to
// Minecraft, so a third game would silently read and write mc_instances. It
// must now refuse instead.
func TestUnregisteredGameHasNoTable(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "tables.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	gmod := domain.GameID("pinball")

	if err := st.upsertInstanceRow(gmod, InstanceRecord{Number: 1, Name: "ttt", Slug: "ttt"}); err == nil {
		t.Error("writing an unregistered game must fail, not land in another game's table")
	}
	if _, err := st.getInstanceRow(gmod, 1); err == nil {
		t.Error("reading an unregistered game must fail")
	}
	if _, err := st.listInstanceRows(gmod); err == nil {
		t.Error("listing an unregistered game must fail")
	}
	if err := st.deleteInstanceRow(gmod, 1); err == nil {
		t.Error("deleting from an unregistered game must fail")
	}

	// Nothing leaked into Minecraft's table.
	recs, err := st.ListInstances()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Errorf("mc_instances gained %d rows from an unregistered game", len(recs))
	}
}

// The two tables share core columns but differ in their game-specific ones.
// Generated SQL must reflect that, or a column lands in the wrong table.
func TestGeneratedSQLMatchesEachTable(t *testing.T) {
	mc, err := tableFor(domain.GameMinecraft)
	if err != nil {
		t.Fatal(err)
	}
	vh, err := tableFor(domain.GameValheim)
	if err != nil {
		t.Fatal(err)
	}

	mcSQL, vhSQL := mc.upsertSQL(), vh.upsertSQL()

	if !strings.Contains(mcSQL, "mc_instances") || !strings.Contains(vhSQL, "valheim_instances") {
		t.Fatal("upsert targets the wrong table")
	}
	for _, col := range []string{"loader", "mc_version", "heap_init_gib", "pack_ref"} {
		if !strings.Contains(mcSQL, col) {
			t.Errorf("minecraft upsert is missing %q", col)
		}
		if strings.Contains(vhSQL, col) {
			t.Errorf("valheim upsert must not carry minecraft column %q", col)
		}
	}
	if !strings.Contains(vhSQL, "password") {
		t.Error("valheim upsert is missing password")
	}
	if strings.Contains(mcSQL, "password") {
		t.Error("minecraft upsert must not carry valheim's password column")
	}
	// Shared columns appear in both.
	for _, col := range []string{"mem_request_gib", "cpu_request_milli", "created_by", "seed", "tier"} {
		if !strings.Contains(mcSQL, col) || !strings.Contains(vhSQL, col) {
			t.Errorf("core column %q missing from one of the tables", col)
		}
	}
	// The creator must survive a later save in both.
	if !strings.Contains(mcSQL, "CASE WHEN mc_instances.created_by") ||
		!strings.Contains(vhSQL, "CASE WHEN valheim_instances.created_by") {
		t.Error("created_by must be first-writer-wins in both tables")
	}
}

func TestRoundTripPreservesGameSpecificFields(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "roundtrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mc := InstanceRecord{
		Number: 1, Name: "World", Slug: "world", Tier: "medium", State: "stopped",
		Loader: "neoforge", MCVersion: "1.21.1", PackRef: "abc", PackProvider: "curseforge",
		Difficulty: "hard", Gamemode: "survival", WorldType: "default",
		HeapInitGiB: 6, MemRequestGiB: 8, MemLimitGiB: 10, CPURequestMilli: 2000,
		Seed: "s1", CreatedBy: "me",
	}
	if err := st.upsertInstanceRow(domain.GameMinecraft, mc); err != nil {
		t.Fatal(err)
	}
	got, err := st.getInstanceRow(domain.GameMinecraft, 1)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.Loader != "neoforge" || got.MCVersion != "1.21.1" || got.HeapInitGiB != 6 ||
		got.Difficulty != "hard" || got.PackProvider != "curseforge" || got.CreatedBy != "me" {
		t.Errorf("minecraft round trip lost fields: %+v", *got)
	}

	vh := InstanceRecord{
		Number: 1, Name: "Odin", Slug: "odin", Tier: "large", State: "running",
		Password: "secret", Seed: "v1", MemRequestGiB: 8, MemLimitGiB: 10, CreatedBy: "me",
	}
	if err := st.upsertInstanceRow(domain.GameValheim, vh); err != nil {
		t.Fatal(err)
	}
	gotVH, err := st.getInstanceRow(domain.GameValheim, 1)
	if err != nil || gotVH == nil {
		t.Fatal(err)
	}
	if gotVH.Password != "secret" || gotVH.Seed != "v1" || gotVH.MemLimitGiB != 10 {
		t.Errorf("valheim round trip lost fields: %+v", *gotVH)
	}
	// Same number, different games, different tables: no collision.
	if gotVH.Name != "Odin" || got.Name != "World" {
		t.Error("instances of the same number collided across games")
	}
}

func TestCreatedByIsFirstWriterWins(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "creator.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	base := InstanceRecord{Number: 1, Name: "W", Slug: "w", Tier: "medium", State: "stopped"}
	base.CreatedBy = "friend"
	if err := st.upsertInstanceRow(domain.GameValheim, base); err != nil {
		t.Fatal(err)
	}
	base.CreatedBy = ""
	base.Name = "Renamed"
	if err := st.upsertInstanceRow(domain.GameValheim, base); err != nil {
		t.Fatal(err)
	}

	got, err := st.getInstanceRow(domain.GameValheim, 1)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.CreatedBy != "friend" {
		t.Errorf("a later save blanked the creator: %q", got.CreatedBy)
	}
	if got.Name != "Renamed" {
		t.Errorf("the save did not apply: %q", got.Name)
	}
}
