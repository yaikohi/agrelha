package store

import (
	"path/filepath"
	"testing"

	"agrelha/internal/domain"
)

func TestInstanceRepo_Minecraft_CRUD(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "repo_mc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	repo := NewInstanceRepo(st)

	// Nil receiver tests
	var nilRepo *InstanceRepo
	if err := nilRepo.Upsert(domain.Instance{}); err != nil {
		t.Errorf("nilRepo.Upsert want nil, got %v", err)
	}
	if inst, err := nilRepo.Get(1); inst != nil || err != nil {
		t.Errorf("nilRepo.Get want nil, got %v, %v", inst, err)
	}
	if list, err := nilRepo.List(); list != nil || err != nil {
		t.Errorf("nilRepo.List want nil, got %v, %v", list, err)
	}
	if err := nilRepo.UpdateState(1, domain.StateRunning); err != nil {
		t.Errorf("nilRepo.UpdateState want nil, got %v", err)
	}
	if err := nilRepo.Delete(1); err != nil {
		t.Errorf("nilRepo.Delete want nil, got %v", err)
	}

	// 1. Get non-existent
	got, err := repo.Get(1)
	if err != nil || got != nil {
		t.Fatalf("Get non-existent want nil, got %v, %v", got, err)
	}

	// 2. Upsert instance with modpack
	inst := domain.Instance{
		GameID:    domain.GameMinecraft,
		Number:    1,
		Name:      "Valhelsia 6",
		Slug:      "valhelsia-6",
		Loader:    domain.LoaderNeoForge,
		Source:    domain.SourceModpack,
		MCVersion: "1.20.1",
		Tier:      domain.TierLarge,
		State:     domain.StateStopped,
		MOTD:      "Welcome to Valhelsia",
		Pack: &domain.Pack{
			Provider: domain.ProviderCurseForge,
			Ref:      "curseforge:12345",
			Name:     "Valhelsia 6 Pack",
		},
	}
	if err := repo.Upsert(inst); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// 3. Get created instance
	got, err = repo.Get(1)
	if err != nil || got == nil {
		t.Fatalf("Get failed: %v, %v", got, err)
	}
	if got.Name != "Valhelsia 6" || got.Pack == nil || got.Pack.Ref != "curseforge:12345" {
		t.Errorf("Get unexpected result: %+v", got)
	}

	// 4. List instances
	list, err := repo.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List want 1 instance, got %d, err=%v", len(list), err)
	}

	// 5. Update state
	if err := repo.UpdateState(1, domain.StateRunning); err != nil {
		t.Fatalf("UpdateState failed: %v", err)
	}
	got, _ = repo.Get(1)
	if got.State != domain.StateRunning {
		t.Errorf("State want running, got %s", got.State)
	}

	// 6. Delete instance
	if err := repo.Delete(1); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	listAfter, err := repo.List()
	if err != nil || len(listAfter) != 0 {
		t.Fatalf("List after delete want 0, got %d", len(listAfter))
	}
}

func TestInstanceRepo_Valheim_CRUD(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "repo_vh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	repo := NewValheimInstanceRepo(st)

	// 1. Get non-existent
	got, err := repo.Get(2)
	if err != nil || got != nil {
		t.Fatalf("Get non-existent want nil, got %v, %v", got, err)
	}

	// 2. Upsert Valheim instance
	inst := domain.Instance{
		GameID:   domain.GameValheim,
		Number:   2,
		Name:     "Lareira",
		Slug:     "lareira",
		Password: "secretpassword",
		Source:   domain.SourceVanilla,
		Tier:     domain.TierMedium,
		State:    domain.StateStopped,
	}
	if err := repo.Upsert(inst); err != nil {
		t.Fatalf("Upsert Valheim failed: %v", err)
	}

	// 3. Get Valheim instance
	got, err = repo.Get(2)
	if err != nil || got == nil {
		t.Fatalf("Get Valheim failed: %v, %v", got, err)
	}
	if got.Name != "Lareira" || got.Password != "secretpassword" || got.Source != domain.SourceVanilla {
		t.Errorf("Get unexpected result: %+v", got)
	}

	// 4. List Valheim instances
	list, err := repo.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List want 1, got %d", len(list))
	}

	// 5. Update state
	if err := repo.UpdateState(2, domain.StateRunning); err != nil {
		t.Fatalf("UpdateState failed: %v", err)
	}
	got, _ = repo.Get(2)
	if got.State != domain.StateRunning {
		t.Errorf("State want running, got %s", got.State)
	}

	// 6. Delete
	if err := repo.Delete(2); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	listAfter, _ := repo.List()
	if len(listAfter) != 0 {
		t.Fatalf("List after delete want 0, got %d", len(listAfter))
	}
}

func TestInstanceRepo_FromRecord_DefaultPackProvider(t *testing.T) {
	rec := InstanceRecord{
		Number:       1,
		Name:         "Modpack Server",
		Source:       string(domain.SourceModpack),
		Pack:         "Some Pack",
		PackProvider: "", // empty should default to CurseForge
		PackRef:      "curseforge:99999",
	}
	inst := fromRecord(rec, domain.GameMinecraft)
	if inst.Pack == nil || inst.Pack.Provider != domain.ProviderCurseForge {
		t.Errorf("expected ProviderCurseForge default, got %+v", inst.Pack)
	}
}
