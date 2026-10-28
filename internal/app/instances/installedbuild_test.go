package instances

import (
	"context"
	"testing"

	"agrelha/internal/domain"
)

type fakeInstalled struct {
	facts domain.PackFacts
	err   error
	calls int
}

func (f *fakeInstalled) InstalledBuild(ctx context.Context, id domain.InstanceID) (domain.PackFacts, error) {
	f.calls++
	return f.facts, f.err
}

func packInstance(version string) *domain.Instance {
	return &domain.Instance{
		Number: 2,
		Source: domain.SourceModpack,
		Minecraft: &domain.MinecraftConfig{
			Loader:    domain.LoaderNeoForge,
			MCVersion: version,
			Pack: &domain.Pack{
				Provider: domain.ProviderCurseForge,
				Ref:      "https://www.curseforge.com/minecraft/modpacks/craftoria-2",
			},
		},
	}
}

// Valeria recorded 1.21.1 from the wizard default while the server had 26.1.2
// installed. What is installed cannot be wrong, so it wins.
func TestRecordedBuildIsCorrectedFromWhatIsInstalled(t *testing.T) {
	repo := &ipRepo{}
	m := &InstanceManager{repo: repo, gameID: domain.GameMinecraft,
		installed: &fakeInstalled{facts: domain.PackFacts{Loader: domain.LoaderNeoForge, MCVersion: "26.1.2"}}}

	inst := packInstance("1.21.1")
	m.reconcileInstalledBuild(context.Background(), inst)

	if inst.Minecraft.MCVersion != "26.1.2" {
		t.Errorf("version = %q, want 26.1.2", inst.Minecraft.MCVersion)
	}
	if len(repo.items) != 1 {
		t.Errorf("the correction must be persisted, got %d upserts", len(repo.items))
	}
}

func TestAnAlreadyCorrectRecordIsNotRewritten(t *testing.T) {
	repo := &ipRepo{}
	m := &InstanceManager{repo: repo, gameID: domain.GameMinecraft,
		installed: &fakeInstalled{facts: domain.PackFacts{Loader: domain.LoaderNeoForge, MCVersion: "26.1.2"}}}

	m.reconcileInstalledBuild(context.Background(), packInstance("26.1.2"))

	if len(repo.items) != 0 {
		t.Errorf("an unchanged build must not write, got %d upserts", len(repo.items))
	}
}

// A Mod list Instance's version is the operator's choice, not a Pack's fact.
func TestAModListInstanceIsNeverCorrected(t *testing.T) {
	installed := &fakeInstalled{facts: domain.PackFacts{Loader: domain.LoaderFabric, MCVersion: "26.3"}}
	m := &InstanceManager{repo: &ipRepo{}, gameID: domain.GameMinecraft, installed: installed}

	inst := &domain.Instance{
		Number:    3,
		Source:    domain.SourceModlist,
		Minecraft: &domain.MinecraftConfig{Loader: domain.LoaderNeoForge, MCVersion: "1.21.1"},
	}
	m.reconcileInstalledBuild(context.Background(), inst)

	if inst.Minecraft.MCVersion != "1.21.1" {
		t.Errorf("version = %q; only a Pack owns this field", inst.Minecraft.MCVersion)
	}
	if installed.calls != 0 {
		t.Error("a Mod list Instance should not even be asked")
	}
}

func TestAnUnreadableManifestLeavesTheRecordAlone(t *testing.T) {
	m := &InstanceManager{repo: &ipRepo{}, gameID: domain.GameMinecraft,
		installed: &fakeInstalled{err: context.DeadlineExceeded}}

	inst := packInstance("1.21.1")
	m.reconcileInstalledBuild(context.Background(), inst)

	if inst.Minecraft.MCVersion != "1.21.1" {
		t.Errorf("version = %q; a failed read must not blank the record", inst.Minecraft.MCVersion)
	}
}
