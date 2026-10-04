package store

import (
	"path/filepath"
	"testing"

	"agrelha/internal/domain"
)

// The whole point of the registry and table refactor: a third game is data, and
// it must not land in another game's table or pick up another game's shape.
func TestGModInstanceStaysInItsOwnTable(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "gmod.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	repo := NewGameInstanceRepo(st, domain.GModProfile)
	inst := domain.Instance{
		GameID: domain.GameGMod, Number: 1, Name: "TTT Night", Slug: "ttt-night",
		Source: domain.SourceModpack, Tier: domain.TierSmall, State: domain.StateStopped,
		GMod: &domain.GModConfig{
			Pack:     &domain.Pack{Provider: domain.ProviderSteamWorkshop, Ref: "104604903", Name: "Annual TTT"},
			Gamemode: "terrortown", Map: "ttt_minecraft_b5", Password: "secret",
		},
	}
	if err := repo.Upsert(inst); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Nothing leaked into the other games' tables.
	if mc, err := st.ListInstances(); err != nil || len(mc) != 0 {
		t.Errorf("mc_instances gained %d rows from a Garry's Mod world", len(mc))
	}
	if vh, err := st.ListValheimInstances(); err != nil || len(vh) != 0 {
		t.Errorf("valheim_instances gained %d rows from a Garry's Mod world", len(vh))
	}

	got, err := repo.Get(1)
	if err != nil || got == nil {
		t.Fatalf("Get: %v", err)
	}
	if got.GMod == nil {
		t.Fatal("GMod config lost on round trip")
	}
	if got.GMod.CollectionID() != "104604903" {
		t.Errorf("collection id = %q, want 104604903", got.GMod.CollectionID())
	}
	if got.GMod.Pack == nil || got.GMod.Pack.Provider != domain.ProviderSteamWorkshop {
		t.Errorf("pack provider lost: %+v", got.GMod.Pack)
	}
	if got.GMod.Gamemode != "terrortown" || got.GMod.Map != "ttt_minecraft_b5" {
		t.Errorf("gamemode/map lost: %q / %q", got.GMod.Gamemode, got.GMod.Map)
	}
	if got.GMod.Password != "secret" {
		t.Errorf("password lost: %q", got.GMod.Password)
	}
	// A Garry's Mod world must not acquire another game's state.
	if got.Minecraft != nil {
		t.Error("a Garry's Mod world was given a Minecraft config")
	}
	if got.Valheim != nil {
		t.Error("a Garry's Mod world was given a Valheim config")
	}
}

// Numbers are only unique within a game, so all three may hold number 1.
func TestThreeGamesShareNumberOne(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "three.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	for _, p := range domain.Profiles() {
		repo := NewGameInstanceRepo(st, p)
		if err := repo.Upsert(domain.Instance{
			GameID: p.ID, Number: 1, Name: string(p.ID) + " one", Slug: "one",
			Tier: domain.TierMedium, State: domain.StateStopped,
		}); err != nil {
			t.Fatalf("upsert %s: %v", p.ID, err)
		}
	}

	for _, p := range domain.Profiles() {
		got, err := NewGameInstanceRepo(st, p).Get(1)
		if err != nil || got == nil {
			t.Fatalf("get %s: %v", p.ID, err)
		}
		if got.Name != string(p.ID)+" one" {
			t.Errorf("%s number 1 returned %q", p.ID, got.Name)
		}
	}
}
