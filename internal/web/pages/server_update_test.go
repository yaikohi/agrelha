package pages

import (
	"context"
	"strings"
	"testing"
)

func renderVDash(t *testing.T, inst InstanceUI) string {
	t.Helper()
	var b strings.Builder
	if err := ValheimDashboard([]InstanceUI{inst}, BudgetUI{MaxInstances: 4}).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func TestServerUpdateBadgeAppearsOnlyWhenBehind(t *testing.T) {
	base := InstanceUI{Number: 1, Name: "lareira-V2", Slug: "lareira-v2", State: "running", Tier: "medium"}

	without := renderVDash(t, base)
	if strings.Contains(without, "Server update") {
		t.Error("no badge when the build matches the store")
	}

	behind := base
	behind.ServerUpdate = true
	with := renderVDash(t, behind)
	if !strings.Contains(with, "Server update") {
		t.Errorf("want the badge when behind, got: %s", with)
	}
}

// CONTEXT.md defines Mod update and Server update as different things. The card
// must be able to show both at once without either being mistaken for the other.
func TestServerUpdateAndModUpdateAreDistinct(t *testing.T) {
	both := InstanceUI{
		Number: 1, Name: "lareira-V2", Slug: "lareira-v2", State: "running", Tier: "medium",
		ServerUpdate: true, ModUpdates: 3,
	}
	got := renderVDash(t, both)
	if !strings.Contains(got, "Server update") {
		t.Error("want the server-update badge")
	}
	if !strings.Contains(got, "3 mod update(s)") {
		t.Error("want the mod-update badge alongside it")
	}
}

func TestOverviewExplainsTheServerUpdateIsAutomatic(t *testing.T) {
	d := InstanceDetailUI{
		InstanceUI: InstanceUI{
			Number: 1, Name: "lareira-V2", Slug: "lareira-v2", State: "running",
			ServerUpdate: true, ServerBuild: "25253791",
		},
		ActiveTab: "overview",
	}
	var b strings.Builder
	if err := ValheimInstanceDetail(d).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	got := b.String()
	for _, want := range []string{
		"Server update available",
		"world saves are not touched",
		"running build 25253791",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("overview missing %q", want)
		}
	}
}
