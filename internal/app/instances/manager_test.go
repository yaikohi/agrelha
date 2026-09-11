package instances

import (
	"agrelha/internal/domain"
	"agrelha/internal/infra/manifests"
	valheimmanifests "agrelha/internal/infra/manifests/valheim"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstanceManagerCRUDAndBudget(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	defer st.Close()

	mgr := NewInstanceManager(
		store.NewInstanceRepo(st), nil, nil, 24, 4, 2, "manifests/minecraft-modded", "192.168.20.224", manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded"), "minecraft-modded")

	ctx := context.Background()

	// 1. Create first instance (auto-number 1)
	inst1, err := mgr.CreateInstance(ctx, domain.Instance{
		Name:      "Fluxweave",
		Loader:    domain.LoaderNeoForge,
		Source:    domain.SourceModlist,
		MCVersion: "1.21.1",
		Tier:      domain.TierLarge, // 12 GiB
	}, "jei\n")
	if err != nil {
		t.Fatalf("create instance 1 failed: %v", err)
	}
	if inst1.Number != 1 {
		t.Fatalf("expected instance number 1, got %d", inst1.Number)
	}
	if inst1.LBIP != "192.168.20.225" {
		t.Fatalf("expected LB IP 192.168.20.225, got %s", inst1.LBIP)
	}

	// 2. Create second instance (auto-number 2)
	inst2, err := mgr.CreateInstance(ctx, domain.Instance{
		Name:      "Vanilla Survival",
		Source:    domain.SourceVanilla,
		MCVersion: "1.21.4",
		Tier:      domain.TierLarge, // 12 GiB
	}, "")
	if err != nil {
		t.Fatalf("create instance 2 failed: %v", err)
	}
	if inst2.Number != 2 {
		t.Fatalf("expected instance number 2, got %d", inst2.Number)
	}
	if inst2.LBIP != "192.168.20.226" {
		t.Fatalf("expected LB IP 192.168.20.226, got %s", inst2.LBIP)
	}

	// 3. Create third instance
	inst3, err := mgr.CreateInstance(ctx, domain.Instance{
		Name:      "Cobblemon",
		Loader:    domain.LoaderFabric,
		Source:    domain.SourceModlist,
		MCVersion: "1.20.1",
		Tier:      domain.TierSmall, // 4 GiB
	}, "fabric-api\n")
	if err != nil {
		t.Fatalf("create instance 3 failed: %v", err)
	}
	if inst3.Number != 3 {
		t.Fatalf("expected instance number 3, got %d", inst3.Number)
	}

	// 4. Test starting instances and enforcing budget
	// Start inst1 (12 GiB)
	if err := mgr.StartInstance(ctx, 1); err != nil {
		t.Fatalf("failed to start inst1: %v", err)
	}

	// Start inst2 (12 GiB, total = 24 GiB, running = 2)
	if err := mgr.StartInstance(ctx, 2); err != nil {
		t.Fatalf("failed to start inst2: %v", err)
	}

	// Starting inst3 should fail because maxRunning is 2
	if err := mgr.StartInstance(ctx, 3); err == nil {
		t.Fatalf("expected starting inst3 to fail due to maxRunning limit")
	}

	// Stop inst2
	if err := mgr.StopInstance(ctx, 2); err != nil {
		t.Fatalf("failed to stop inst2: %v", err)
	}

	// Now starting inst3 (4 GiB) should succeed (12 + 4 = 16 <= 24, running = 2)
	if err := mgr.StartInstance(ctx, 3); err != nil {
		t.Fatalf("failed to start inst3: %v", err)
	}

	// Budget check
	list, err := mgr.ListInstances(ctx)
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 instances, got %d", len(list))
	}
	b := mgr.Budget(list)
	if b.UsedGiB != 16 {
		t.Fatalf("expected 16 GiB used, got %d", b.UsedGiB)
	}
	if b.RunningCount != 2 {
		t.Fatalf("expected 2 running, got %d", b.RunningCount)
	}

	// 5. Delete inst2 (which is stopped)
	if err := mgr.DeleteInstance(ctx, 2); err != nil {
		t.Fatalf("failed to delete stopped inst2: %v", err)
	}

	listAfter, err := mgr.ListInstances(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(listAfter) != 2 {
		t.Fatalf("expected 2 instances after delete, got %d", len(listAfter))
	}
}

type mockStateStore struct {
	docs map[string]ports.Document
}

func newMockStateStore() *mockStateStore {
	return &mockStateStore{docs: make(map[string]ports.Document)}
}

func (m *mockStateStore) Get(ctx context.Context, path string) (ports.Document, error) {
	doc, ok := m.docs[path]
	if !ok {
		return ports.Document{Data: make(map[string]string)}, nil
	}
	return doc, nil
}

func (m *mockStateStore) Put(ctx context.Context, path string, doc ports.Document, msg string) error {
	m.docs[path] = doc
	return nil
}

func (m *mockStateStore) Patch(ctx context.Context, path string, msg string, mutate func(doc *ports.Document) (bool, error)) (bool, error) {
	doc, ok := m.docs[path]
	if !ok {
		doc = ports.Document{Data: make(map[string]string)}
	}
	changed, err := mutate(&doc)
	if err != nil {
		return false, err
	}
	if changed {
		m.docs[path] = doc
	}
	return changed, nil
}

func (m *mockStateStore) Delete(ctx context.Context, path string, msg string) error {
	delete(m.docs, path)
	return nil
}

func (m *mockStateStore) PutTree(ctx context.Context, dirPath string, docs map[string]ports.Document, msg string) error {
	for k, v := range docs {
		m.docs[dirPath+"/"+k] = v
	}
	return nil
}

type mockAuditRecorder struct {
	records []string
}

func (a *mockAuditRecorder) RecordAudit(actor, action, detail string) error {
	a.records = append(a.records, actor+":"+action+":"+detail)
	return nil
}

func TestInstanceManagerDeepWorkflows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "deep_test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	audit := &mockAuditRecorder{}
	state := newMockStateStore()
	ctx := context.Background()

	mgr := NewInstanceManager(
		store.NewInstanceRepo(st), state, nil, 24, 4, 2, "manifests/minecraft-modded", "192.168.20.224", manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded"), "minecraft-modded",
		WithAudit(audit),
		WithCommandExecutor(func(ctx context.Context, inst domain.Instance, cmd string) (string, error) {
			return "executed: " + cmd, nil
		}),
		WithDependencyResolver(func(ctx context.Context, slug, mcVersion, loader string) ([]string, error) {
			if slug == "create" {
				return []string{"flywheel"}, nil
			}
			return nil, nil
		}),
	)

	// Create
	inst, err := mgr.CreateInstance(ctx, domain.Instance{
		Name:      "Deep World",
		Loader:    domain.LoaderNeoForge,
		Source:    domain.SourceModlist,
		MCVersion: "1.21.1",
		Tier:      domain.TierMedium,
	}, "jei\n", "alice")
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	// UpdateSettings
	err = mgr.UpdateSettings(ctx, inst.Number, "Deep World Renamed", "motd", "large", "1.21.1", "alice")
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	updated, err := mgr.GetInstance(ctx, inst.Number)
	if err != nil || updated.Name != "Deep World Renamed" || updated.Tier != domain.TierLarge {
		t.Fatalf("settings not applied: %+v", updated)
	}

	// Mod Install with dep
	added, err := mgr.InstallMod(ctx, inst.Number, "create", "alice")
	if err != nil {
		t.Fatalf("install mod: %v", err)
	}
	if added != 2 { // create + flywheel
		t.Fatalf("expected 2 added mods, got %d", added)
	}

	// GetInstalledMods
	mods, err := mgr.GetInstalledMods(ctx, inst.Number)
	if err != nil {
		t.Fatalf("get mods: %v", err)
	}
	if len(mods) != 2 || mods[0] != "create" || mods[1] != "flywheel" {
		t.Fatalf("unexpected mods: %v", mods)
	}

	// RemoveMod
	err = mgr.RemoveMod(ctx, inst.Number, "flywheel", "alice")
	if err != nil {
		t.Fatalf("remove mod: %v", err)
	}
	modsAfter, err := mgr.GetInstalledMods(ctx, inst.Number)
	if err != nil || len(modsAfter) != 1 || modsAfter[0] != "create" {
		t.Fatalf("unexpected mods after remove: %v", modsAfter)
	}

	// Instance Configs
	saved, err := mgr.SaveConfig(ctx, inst.Number, "server.properties", "difficulty=hard\n", "alice")
	if err != nil || !saved {
		t.Fatalf("save config failed: %v", err)
	}
	content, err := mgr.GetConfig(ctx, inst.Number, "server.properties")
	if err != nil || content != "difficulty=hard\n" {
		t.Fatalf("get config unexpected: %q", content)
	}
	cfgList, err := mgr.ListConfigs(ctx, inst.Number)
	if err != nil || len(cfgList) != 1 || cfgList[0] != "server.properties" {
		t.Fatalf("list configs unexpected: %v", cfgList)
	}
	deleted, err := mgr.DeleteConfig(ctx, inst.Number, "server.properties", "alice")
	if err != nil || !deleted {
		t.Fatalf("delete config failed: %v", err)
	}

	// Global Configs
	savedG, err := mgr.SaveGlobalConfig(ctx, "global.toml", "foo = bar", "alice")
	if err != nil || !savedG {
		t.Fatalf("save global config failed: %v", err)
	}
	contentG, err := mgr.GetGlobalConfig(ctx, "global.toml")
	if err != nil || contentG != "foo = bar" {
		t.Fatalf("get global config unexpected: %q", contentG)
	}
	listG, err := mgr.ListGlobalConfigs(ctx)
	if err != nil || len(listG) != 1 || listG[0] != "global.toml" {
		t.Fatalf("list global configs unexpected: %v", listG)
	}
	delG, err := mgr.DeleteGlobalConfig(ctx, "global.toml", "alice")
	if err != nil || !delG {
		t.Fatalf("delete global config failed: %v", err)
	}

	// ExecuteCommand
	cmdResp, err := mgr.ExecuteCommand(ctx, inst.Number, "say hello")
	if err != nil || cmdResp != "executed: say hello" {
		t.Fatalf("execute command failed: %v, resp: %s", err, cmdResp)
	}

	// Verify audit records exist
	if len(audit.records) == 0 {
		t.Fatalf("expected audit records, got none")
	}
}

func TestValheimInstanceManager(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test-valheim.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	defer st.Close()

	state := newMockStateStore()
	audit := &mockAuditRecorder{}

	valheimRepo := store.NewValheimInstanceRepo(st)
	renderer := valheimmanifests.New("dedicated=gameserver", "valheim")

	mgr := NewInstanceManager(
		valheimRepo, state, nil, 16, 4, 2,
		"manifests/valheim", "192.168.20.210", renderer, "valheim",
		WithGameID(domain.GameValheim),
		WithAudit(audit),
	)

	ctx := context.Background()

	// 1. Create first Valheim instance
	inst1, err := mgr.CreateInstance(ctx, domain.Instance{
		Name:     "Viking Outpost",
		Password: "outpostpassword",
		Seed:     "seed999",
		Tier:     domain.TierMedium, // 6 GiB
	}, "denikson/BepInExPack_Valheim\n", "admin@agrelha.local")
	if err != nil {
		t.Fatalf("create valheim instance failed: %v", err)
	}

	if inst1.Number != 1 {
		t.Fatalf("expected slot 1, got %d", inst1.Number)
	}
	if inst1.GameID != domain.GameValheim {
		t.Fatalf("expected GameValheim, got %s", inst1.GameID)
	}
	if inst1.DeploymentName() != "valheim-viking-outpost-01" {
		t.Fatalf("unexpected deployment name: %s", inst1.DeploymentName())
	}
	if inst1.LBIP != "192.168.20.211" {
		t.Fatalf("unexpected LBIP: %s", inst1.LBIP)
	}

	// Verify manifests written to stateStore
	depDoc, ok := state.docs["manifests/valheim/instance-01/deployment.yaml"]
	if !ok || !strings.Contains(string(depDoc.Raw), "lloesche/valheim-server:latest") {
		t.Fatalf("deployment.yaml not written to stateStore or missing image: %s", string(depDoc.Raw))
	}
	svcDoc, ok := state.docs["manifests/valheim/instance-01/service.yaml"]
	if !ok || !strings.Contains(string(svcDoc.Raw), "192.168.20.211") {
		t.Fatalf("service.yaml not written to stateStore or missing IP: %s", string(svcDoc.Raw))
	}

	// 2. Create second Valheim instance
	inst2, err := mgr.CreateInstance(ctx, domain.Instance{
		Name:     "Farms of Valheim",
		Password: "farmspassword",
		Tier:     domain.TierLarge, // 8 GiB
	}, "", "admin@agrelha.local")
	if err != nil {
		t.Fatalf("create second instance failed: %v", err)
	}
	if inst2.Number != 2 {
		t.Fatalf("expected slot 2, got %d", inst2.Number)
	}

	// 3. Start instances and verify budget
	if err := mgr.StartInstance(ctx, inst1.Number, "admin@agrelha.local"); err != nil {
		t.Fatalf("start inst1 failed: %v", err)
	}
	if err := mgr.StartInstance(ctx, inst2.Number, "admin@agrelha.local"); err != nil {
		t.Fatalf("start inst2 failed: %v", err)
	}

	// Try to start a 3rd instance when maxRunning is 2
	inst3, err := mgr.CreateInstance(ctx, domain.Instance{
		Name: "Third Instance",
		Tier: domain.TierSmall, // 4 GiB
	}, "")
	if err != nil {
		t.Fatalf("create inst3 failed: %v", err)
	}
	if err := mgr.StartInstance(ctx, inst3.Number); err == nil || !strings.Contains(err.Error(), "maximum of 2 running instances reached") {
		t.Fatalf("expected maxRunning error, got %v", err)
	}

	// 4. Update Valheim Settings
	if err := mgr.UpdateValheimSettings(ctx, inst1.Number, "Viking Stronghold", "Updated MOTD", "large", "newpass", "admin@agrelha.local"); err != nil {
		t.Fatalf("UpdateValheimSettings failed: %v", err)
	}
	updated, err := mgr.GetInstance(ctx, inst1.Number)
	if err != nil || updated == nil {
		t.Fatalf("get updated instance failed: %v", err)
	}
	if updated.Name != "Viking Stronghold" || updated.Password != "newpass" || updated.Tier != domain.TierLarge {
		t.Fatalf("updated instance unexpected: %+v", updated)
	}

	// 5. ListInstancesByGame
	valheimList, err := mgr.ListInstancesByGame(ctx, domain.GameValheim)
	if err != nil {
		t.Fatalf("ListInstancesByGame failed: %v", err)
	}
	if len(valheimList) != 3 {
		t.Fatalf("expected 3 valheim instances, got %d", len(valheimList))
	}

	// 6. Stop and delete
	if err := mgr.StopInstance(ctx, inst2.Number, "admin@agrelha.local"); err != nil {
		t.Fatalf("stop inst2 failed: %v", err)
	}
	if err := mgr.DeleteInstance(ctx, inst2.Number, "admin@agrelha.local"); err != nil {
		t.Fatalf("delete inst2 failed: %v", err)
	}
	deleted, _ := mgr.GetInstance(ctx, inst2.Number)
	if deleted != nil {
		t.Fatalf("expected deleted instance to be nil")
	}

	// Check audit events recorded with valheim prefix
	foundValheimCreate := false
	for _, rec := range audit.records {
		if strings.Contains(rec, "valheim-instance-create") {
			foundValheimCreate = true
		}
	}
	if !foundValheimCreate {
		t.Fatalf("expected valheim-instance-create audit action, got: %+v", audit.records)
	}
}

type ipRepo struct{ items []domain.Instance }

func (r *ipRepo) Upsert(i domain.Instance) error { r.items = append(r.items, i); return nil }
func (r *ipRepo) Get(n int) (*domain.Instance, error) {
	for i := range r.items {
		if r.items[i].Number == n {
			return &r.items[i], nil
		}
	}
	return nil, nil
}
func (r *ipRepo) List() ([]domain.Instance, error)            { return r.items, nil }
func (r *ipRepo) UpdateState(int, domain.InstanceState) error { return nil }
func (r *ipRepo) Delete(int) error                            { return nil }

func TestLBIPCollisionAcrossGamesIsRefused(t *testing.T) {
	repo := &ipRepo{items: []domain.Instance{
		{GameID: domain.GameValheim, Number: 2, Name: "boppo", LBIP: "192.168.20.227"},
	}}
	m := &InstanceManager{repo: repo, gameID: domain.GameMinecraft}

	clash := domain.Instance{GameID: domain.GameMinecraft, Number: 3, Name: "bob", LBIP: "192.168.20.227"}
	err := m.checkLBIPFree(clash)
	if err == nil {
		t.Fatal("a duplicate LB IP must be refused: cilium leaves the service <pending> with no error visible in agrelha")
	}
	if !strings.Contains(err.Error(), "boppo") {
		t.Errorf("the error must name the instance holding the address, got: %v", err)
	}

	free := domain.Instance{GameID: domain.GameMinecraft, Number: 3, Name: "bob", LBIP: "192.168.20.243"}
	if err := m.checkLBIPFree(free); err != nil {
		t.Errorf("a free address must be allowed: %v", err)
	}

	same := domain.Instance{GameID: domain.GameValheim, Number: 2, Name: "boppo", LBIP: "192.168.20.227"}
	if err := m.checkLBIPFree(same); err != nil {
		t.Errorf("an instance must not collide with itself on update: %v", err)
	}
}
