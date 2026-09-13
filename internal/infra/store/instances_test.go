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
