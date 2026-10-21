package store

import (
	"path/filepath"
	"testing"

	"agrelha/internal/domain"
)

func TestInstanceWithNoHostDefaultsToCluster(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "host_default.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	repo := NewGameInstanceRepo(st, domain.ValheimProfile)
	inst := domain.Instance{
		GameID: domain.GameValheim, Number: 1, Name: "Odin", Slug: "odin",
		Tier: domain.TierMedium, State: domain.StateStopped,
	}
	if err := repo.Upsert(inst); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := repo.Get(1)
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if got.Host != domain.HostCluster {
		t.Errorf("Host = %q, want %q", got.Host, domain.HostCluster)
	}
	if !got.Host.IsCluster() {
		t.Error("an Instance that never declared a Host must read as cluster")
	}
}

func TestExplicitHostRoundTrips(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "host_explicit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	repo := NewGameInstanceRepo(st, domain.ValheimProfile)
	inst := domain.Instance{
		GameID: domain.GameValheim, Number: 2, Name: "Fogo", Slug: "fogo",
		Tier: domain.TierMedium, State: domain.StateStopped,
		Host: domain.Host("fogo"),
	}
	if err := repo.Upsert(inst); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := repo.Get(2)
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if got.Host != domain.Host("fogo") {
		t.Errorf("Host = %q, want fogo", got.Host)
	}
	if got.Host.IsCluster() {
		t.Error("a world on another Host must not report as cluster")
	}
}

func TestHostSurvivesAnUpdateThatDoesNotMentionIt(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "host_update.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	repo := NewGameInstanceRepo(st, domain.ValheimProfile)
	inst := domain.Instance{
		GameID: domain.GameValheim, Number: 3, Name: "Fogo", Slug: "fogo",
		Tier: domain.TierMedium, State: domain.StateStopped,
		Host: domain.Host("fogo"),
	}
	if err := repo.Upsert(inst); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	stored, err := repo.Get(3)
	if err != nil || stored == nil {
		t.Fatalf("get: %v", err)
	}
	stored.State = domain.StateRunning
	if err := repo.Upsert(*stored); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}

	got, err := repo.Get(3)
	if err != nil || got == nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.Host != domain.Host("fogo") {
		t.Errorf("Host = %q after a state change, want fogo; a lifecycle write must not relocate a world", got.Host)
	}
}
