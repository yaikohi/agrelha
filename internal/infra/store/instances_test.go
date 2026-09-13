package store

import (
	"path/filepath"
	"testing"
)

func TestValheimSourceSurvivesARoundTrip(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "vh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.UpsertValheimInstance(InstanceRecord{
		Number: 1, Name: "lareira-V2", Slug: "lareira-v2", Tier: "large", State: "running",
		Source: "vanilla",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetValheimInstance(1)
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.Source != "vanilla" {
		t.Errorf("Source = %q, want vanilla — without it every world renders as Modded", got.Source)
	}
	all, err := st.ListValheimInstances()
	if err != nil || len(all) != 1 || all[0].Source != "vanilla" {
		t.Errorf("list lost Source: %+v %v", all, err)
	}
}

func TestMissingSourceDistinguishesUnsetFromModlist(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "src.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Written before Source existed: the column is empty.
	if _, err := st.DB().Exec(
		`INSERT INTO valheim_instances (number, name, slug, tier, state) VALUES (1, 'lareira-V2', 'lareira-v2', 'large', 'running')`); err != nil {
		t.Fatal(err)
	}
	// Written since: explicitly modded.
	if err := st.UpsertValheimInstance(InstanceRecord{
		Number: 2, Name: "boppo", Slug: "boppo", Tier: "large", State: "stopped", Source: "modlist",
	}); err != nil {
		t.Fatal(err)
	}

	missing, err := st.ValheimInstancesMissingSource()
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != 1 {
		t.Fatalf("got %v, want [1] — the repository normalises '' to modlist, so only the store can tell them apart", missing)
	}
}
