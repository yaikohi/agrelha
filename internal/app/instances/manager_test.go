package instances

import (
	"agrelha/internal/domain"
	"agrelha/internal/infra/manifests"
	"context"
	"path/filepath"
	"testing"

	"agrelha/internal/infra/store"
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
