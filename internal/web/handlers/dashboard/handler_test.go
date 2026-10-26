package dashboard

import (
	"testing"

	"agrelha/internal/domain"
)

func TestSummaryCarriesTheMinecraftPack(t *testing.T) {
	inst := domain.Instance{
		GameID: domain.GameMinecraft, Number: 1, Name: "Create Live",
		Source: domain.SourceModpack,
		Minecraft: &domain.MinecraftConfig{
			Loader:    domain.LoaderNeoForge,
			MCVersion: "1.21.1",
			Pack:      &domain.Pack{Provider: domain.ProviderModrinth, Ref: "create-arcane", Name: "Create: Arcane Engineering"},
		},
	}

	if got := mcPackName(inst); got != "Create: Arcane Engineering" {
		t.Errorf(`pack name = %q.

A modpack world whose pack never reaches the view model looks mod-less: the
dashboard then hides the .mrpack download that is the whole point of the world.`, got)
	}
	if got := mcPackRef(inst); got != "create-arcane" {
		t.Errorf("pack ref = %q, want create-arcane", got)
	}
	if got := mcPackProvider(inst); got != string(domain.ProviderModrinth) {
		t.Errorf("pack provider = %q", got)
	}

	bare := domain.Instance{GameID: domain.GameMinecraft, Number: 2}
	if mcPackName(bare) != "" || mcPackRef(bare) != "" || mcPackProvider(bare) != "" {
		t.Error("a world with no pack must report no pack")
	}
}
