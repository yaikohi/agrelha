package content

import (
	"context"
	"testing"

	"agrelha/internal/domain"
)

type mockProjectResolver struct {
	projects []domain.ModProject
}

func (m *mockProjectResolver) GetProjects(ctx context.Context, idsOrSlugs []string) ([]domain.ModProject, error) {
	return m.projects, nil
}

func TestCheckCartCompatibility(t *testing.T) {
	resolver := &mockProjectResolver{
		projects: []domain.ModProject{
			{Slug: "jei", Loaders: []string{"neoforge", "fabric"}},
			{Slug: "ferrite-core", Loaders: []string{"neoforge"}},
			{Slug: "sodium", Loaders: []string{"fabric"}},
		},
	}

	compat := CheckCartCompatibility(context.Background(), resolver, []string{"jei", "ferrite-core", "sodium"}, "1.21.1")
	if compat.TotalMods != 3 {
		t.Errorf("TotalMods = %d, want 3", compat.TotalMods)
	}
	if compat.NeoForgeFit != 2 {
		t.Errorf("NeoForgeFit = %d, want 2", compat.NeoForgeFit)
	}
	if compat.FabricFit != 2 {
		t.Errorf("FabricFit = %d, want 2", compat.FabricFit)
	}
	if compat.BestLoader != "neoforge" {
		t.Errorf("BestLoader = %s, want neoforge", compat.BestLoader)
	}
	if len(compat.NeoBlocking) != 1 || compat.NeoBlocking[0] != "sodium" {
		t.Errorf("NeoBlocking = %v, want [sodium]", compat.NeoBlocking)
	}
	if len(compat.FabBlocking) != 1 || compat.FabBlocking[0] != "ferrite-core" {
		t.Errorf("FabBlocking = %v, want [ferrite-core]", compat.FabBlocking)
	}
}

func TestCheckCartCompatibilityEmpty(t *testing.T) {
	compat := CheckCartCompatibility(context.Background(), nil, nil, "1.21.1")
	if compat.BestLoader != "neoforge" {
		t.Errorf("BestLoader = %s, want neoforge", compat.BestLoader)
	}
}

type errResolver struct{}

func (e *errResolver) GetProjects(ctx context.Context, idsOrSlugs []string) ([]domain.ModProject, error) {
	return nil, context.Canceled
}

func TestCheckCartCompatibilityFabricAndError(t *testing.T) {
	// 1. Error / empty projects fallback
	errCompat := CheckCartCompatibility(context.Background(), &errResolver{}, []string{"mod1", "mod2"}, "1.21.1")
	if errCompat.BestLoader != "neoforge" || errCompat.TotalMods != 2 {
		t.Errorf("unexpected error fallback: %+v", errCompat)
	}

	// 2. FabricFit > NeoForgeFit -> BestLoader = "fabric"
	resolver := &mockProjectResolver{
		projects: []domain.ModProject{
			{Slug: "sodium", Loaders: []string{"fabric"}},
			{Slug: "lithium", Loaders: []string{"fabric"}},
		},
	}
	fabCompat := CheckCartCompatibility(context.Background(), resolver, []string{"sodium", "lithium"}, "1.21.1")
	if fabCompat.BestLoader != "fabric" {
		t.Errorf("BestLoader = %s, want fabric", fabCompat.BestLoader)
	}
}

