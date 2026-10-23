package instances

import (
	"context"
	"path/filepath"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/infra/manifests"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
)

func hostScopedReaderManager(t *testing.T) (*InstanceManager, *mockStateStore) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "readers.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	states := newMockStateStore()
	mgr := NewInstanceManager(
		store.NewGameInstanceRepo(st, domain.ValheimProfile), states, nil, 64, 4, 4,
		"manifests/valheim", "192.168.20.224",
		manifests.New("ykhi.xyz/gameserver=true", "valheim"), "valheim",
		WithGameID(domain.GameValheim),
		WithGameProfile(domain.ValheimProfile),
		WithModsReader(func(context.Context, int) ([]string, error) {
			return []string{"Advize/PlantEverything/1.21.3"}, nil
		}),
		WithConfigsReader(func(context.Context, int) (map[string]string, error) {
			return map[string]string{"cluster.cfg": "from the cluster"}, nil
		}),
	)
	return mgr, states
}

func seedHostScopedWorld(t *testing.T, mgr *InstanceManager, states *mockStateStore, num int, host domain.Host) {
	t.Helper()
	inst := domain.Instance{
		Number: num,
		Name:   "Lareira",
		Slug:   "lareira",
		Tier:   domain.TierSmall,
		Host:   host,
		GameID: domain.GameValheim,
	}
	if err := mgr.repo.Upsert(inst); err != nil {
		t.Fatalf("seed instance %d: %v", num, err)
	}
	dir := mgr.instanceDirFor(inst)
	_ = states.Put(context.Background(), dir+"/mods.yaml", ports.Document{
		Data: map[string]string{"mods.txt": "Advize/PlantEverything/1.21.4\n"},
	}, "seed")
	_ = states.Put(context.Background(), dir+"/configs.yaml", ports.Document{
		Data: map[string]string{"host.cfg": "from the state store"},
	}, "seed")
}

func TestADockerHostWorldReadsItsModListFromTheStateStore(t *testing.T) {
	mgr, states := hostScopedReaderManager(t)
	seedHostScopedWorld(t, mgr, states, 1, "fogo")

	got, err := mgr.GetInstalledMods(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetInstalledMods: %v", err)
	}
	if len(got) != 1 || got[0] != "Advize/PlantEverything/1.21.4" {
		t.Errorf("a Docker Host world read its Mod list from the Kubernetes reader, which ArgoCD stopped updating at the migration;\nthe Desired state in the state store says 1.21.4 and the cluster ConfigMap still says 1.21.3,\nso the update checker reports an update that applying can never clear.\ngot %v", got)
	}
}

func TestAClusterWorldStillReadsItsModListFromTheCluster(t *testing.T) {
	mgr, states := hostScopedReaderManager(t)
	seedHostScopedWorld(t, mgr, states, 2, domain.HostCluster)

	got, err := mgr.GetInstalledMods(context.Background(), 2)
	if err != nil {
		t.Fatalf("GetInstalledMods: %v", err)
	}
	if len(got) != 1 || got[0] != "Advize/PlantEverything/1.21.3" {
		t.Errorf("a cluster world must keep reading the live ConfigMap: the cluster materialises mods there,\nand the state store only holds what ArgoCD has yet to sync.\ngot %v", got)
	}
}

func TestADockerHostWorldReadsItsConfigsFromTheStateStore(t *testing.T) {
	mgr, states := hostScopedReaderManager(t)
	seedHostScopedWorld(t, mgr, states, 1, "fogo")
	ctx := context.Background()

	data, err := mgr.ListConfigData(ctx, 1)
	if err != nil {
		t.Fatalf("ListConfigData: %v", err)
	}
	if _, ok := data["host.cfg"]; !ok {
		t.Errorf("ListConfigData served the cluster reader for a Docker Host world; got %v", data)
	}

	inst, err := mgr.repo.Get(1)
	if err != nil || inst == nil {
		t.Fatalf("repo.Get: %v", err)
	}
	forData, err := mgr.ListConfigDataFor(ctx, *inst)
	if err != nil {
		t.Fatalf("ListConfigDataFor: %v", err)
	}
	if _, ok := forData["host.cfg"]; !ok {
		t.Errorf("ListConfigDataFor served the cluster reader for a Docker Host world; got %v", forData)
	}

	files, err := mgr.ListConfigs(ctx, 1)
	if err != nil {
		t.Fatalf("ListConfigs: %v", err)
	}
	if len(files) != 1 || files[0] != "host.cfg" {
		t.Errorf("ListConfigs served the cluster reader for a Docker Host world; got %v", files)
	}
}
