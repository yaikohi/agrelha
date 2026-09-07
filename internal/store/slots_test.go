package store

import (
	"path/filepath"
	"testing"
)

func TestSlotUpsertPreservesMetadata(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertSlot(Slot{
		Slot: "ducktopia", Type: "AUTO_CURSEFORGE",
		Pack: "Ducktopia Farlands", CFPageURL: "https://cf/ducktopia",
	}); err != nil {
		t.Fatal(err)
	}

	// A later touch with empty metadata must not wipe what we already know.
	if err := st.UpsertSlot(Slot{Slot: "ducktopia", Type: "AUTO_CURSEFORGE"}); err != nil {
		t.Fatal(err)
	}

	rows, err := st.ListSlots()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d slots, want 1", len(rows))
	}
	if rows[0].Pack != "Ducktopia Farlands" {
		t.Fatalf("pack lost on re-upsert: %q", rows[0].Pack)
	}
	if rows[0].CFPageURL != "https://cf/ducktopia" {
		t.Fatalf("cf url lost on re-upsert: %q", rows[0].CFPageURL)
	}
}

func TestListSlotsOrdersByLastUsed(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	for _, s := range []string{"alpha", "beta"} {
		if err := st.UpsertSlot(Slot{Slot: s, Type: "NEOFORGE"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.db.Exec(`UPDATE mc_slots SET last_used = '2020-01-01 00:00:00' WHERE slot = 'beta'`); err != nil {
		t.Fatal(err)
	}

	rows, err := st.ListSlots()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Slot != "alpha" {
		t.Fatalf("want most-recent first, got %+v", rows)
	}

	if err := st.DeleteSlot("beta"); err != nil {
		t.Fatal(err)
	}
	if rows, _ = st.ListSlots(); len(rows) != 1 {
		t.Fatalf("delete failed, %d rows remain", len(rows))
	}
}
