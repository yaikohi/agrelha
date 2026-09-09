package instances

import (
	"agrelha/internal/domain"
	"agrelha/internal/infra/manifests"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
	"context"
	"path/filepath"
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
