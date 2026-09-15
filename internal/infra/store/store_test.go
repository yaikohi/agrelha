package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func TestOpenAndMigrateIdempotency(t *testing.T) {
	s := newTestStore(t)

	// Calling migrate again on an already-migrated database should be idempotent
	// and not fail on duplicate column / duplicate table errors.
	if err := s.migrate(); err != nil {
		t.Fatalf("second migrate() failed: %v", err)
	}
}

func TestUpsertSeenAndPlayerRoster(t *testing.T) {
	s := newTestStore(t)

	// 1. First seen with a character name
	if err := s.UpsertSeen("76561198000000001", "Thor", true); err != nil {
		t.Fatalf("UpsertSeen initial failed: %v", err)
	}

	players, err := s.ListPlayers()
	if err != nil {
		t.Fatalf("ListPlayers failed: %v", err)
	}
	if len(players) != 1 {
		t.Fatalf("expected 1 player, got %d", len(players))
	}
	if players[0].Character != "Thor" || players[0].Sessions != 1 {
		t.Fatalf("expected Thor with 1 session, got %+v", players[0])
	}

	// 2. Critical test: second seen with an EMPTY character name.
	// COALESCE(NULLIF(excluded.character, ''), players.character) must preserve "Thor",
	// not wipe it to empty string.
	if err := s.UpsertSeen("76561198000000001", "", true); err != nil {
		t.Fatalf("UpsertSeen empty character failed: %v", err)
	}

	players, err = s.ListPlayers()
	if err != nil {
		t.Fatalf("ListPlayers failed: %v", err)
	}
	if players[0].Character != "Thor" {
		t.Fatalf("character was wiped by empty string; expected Thor, got %q", players[0].Character)
	}
	if players[0].Sessions != 2 {
		t.Fatalf("expected 2 sessions after bump, got %d", players[0].Sessions)
	}

	// 3. Update with a new non-empty character name without session bump
	if err := s.UpsertSeen("76561198000000001", "Odin", false); err != nil {
		t.Fatalf("UpsertSeen update character failed: %v", err)
	}

	players, err = s.ListPlayers()
	if err != nil {
		t.Fatalf("ListPlayers failed: %v", err)
	}
	if players[0].Character != "Odin" {
		t.Fatalf("expected updated character Odin, got %q", players[0].Character)
	}
	if players[0].Sessions != 2 {
		t.Fatalf("sessions should remain 2 when bumpSession=false, got %d", players[0].Sessions)
	}
}

func TestPresence(t *testing.T) {
	s := newTestStore(t)

	count, err := s.CountOnline()
	if err != nil {
		t.Fatalf("CountOnline failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 online, got %d", count)
	}

	// Set online
	if err := s.SetOnline("steam1", true); err != nil {
		t.Fatalf("SetOnline(steam1, true) failed: %v", err)
	}
	if err := s.SetOnline("steam2", true); err != nil {
		t.Fatalf("SetOnline(steam2, true) failed: %v", err)
	}
	count, _ = s.CountOnline()
	if count != 2 {
		t.Fatalf("expected 2 online, got %d", count)
	}

	// Set steam1 offline
	if err := s.SetOnline("steam1", false); err != nil {
		t.Fatalf("SetOnline(steam1, false) failed: %v", err)
	}
	count, _ = s.CountOnline()
	if count != 1 {
		t.Fatalf("expected 1 online, got %d", count)
	}

	// Clear presence
	if err := s.ClearPresence(); err != nil {
		t.Fatalf("ClearPresence failed: %v", err)
	}
	count, _ = s.CountOnline()
	if count != 0 {
		t.Fatalf("expected 0 online after ClearPresence, got %d", count)
	}
}

func TestInstanceCRUD(t *testing.T) {
	s := newTestStore(t)

	// 1. Get nonexistent instance
	rec, err := s.GetInstance(1)
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}
	if rec != nil {
		t.Fatalf("expected nil for nonexistent instance, got %+v", rec)
	}

	// 2. Upsert instance
	initial := InstanceRecord{
		Number:       1,
		Name:         "World 01",
		Slug:         "world-01",
		Seed:         "12345",
		Loader:       "neoforge",
		Source:       "modpack",
		Pack:         "AllTheMods",
		PackProvider: "curseforge",
		PackRef:      "https://curseforge.com/...",
		MCVersion:    "1.21.1",
		Tier:         "medium",
		State:        "stopped",
		MOTD:         "Welcome to World 01",
		Difficulty:   "hard",
		Gamemode:     "survival",
		WorldType:    "default",
		MaxPlayers:   15,
		LBIP:         "192.168.20.225",
	}

	if err := s.UpsertInstance(initial); err != nil {
		t.Fatalf("UpsertInstance failed: %v", err)
	}

	got, err := s.GetInstance(1)
	if err != nil {
		t.Fatalf("GetInstance(1) failed: %v", err)
	}
	if got == nil {
		t.Fatalf("expected instance 1, got nil")
	}
	if got.Name != initial.Name || got.Slug != initial.Slug || got.Pack != initial.Pack || got.MaxPlayers != 15 {
		t.Fatalf("retrieved instance fields mismatch: got %+v, want %+v", got, initial)
	}

	// 3. Update state
	if err := s.UpdateInstanceState(1, "running"); err != nil {
		t.Fatalf("UpdateInstanceState failed: %v", err)
	}
	got, _ = s.GetInstance(1)
	if got.State != "running" {
		t.Fatalf("expected state running, got %q", got.State)
	}

	// 4. List instances
	list, err := s.ListInstances()
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}
	if len(list) != 1 || list[0].Number != 1 {
		t.Fatalf("unexpected ListInstances output: %+v", list)
	}

	// 5. Delete instance
	if err := s.DeleteInstance(1); err != nil {
		t.Fatalf("DeleteInstance failed: %v", err)
	}
	got, _ = s.GetInstance(1)
	if got != nil {
		t.Fatalf("expected nil after delete, got %+v", got)
	}
}

func TestValheimInstancesStoreAndRepo(t *testing.T) {
	s := newTestStore(t)
	repo := NewValheimInstanceRepo(s)

	inst := domain.Instance{
		GameID:     domain.GameValheim,
		Number:     1,
		Name:       "Odin's Hall",
		Slug:       "odins-hall",
		Seed:       "seed123",
		Password:   "valheimpass",
		Tier:       domain.TierLarge,
		State:      domain.StateRunning,
		MOTD:       "Welcome to Valheim",
		MaxPlayers: 10,
		LBIP:       "192.168.20.211",
	}

	// 1. Upsert via repo
	if err := repo.Upsert(inst); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// 2. Get via repo
	got, err := repo.Get(1)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got == nil {
		t.Fatalf("expected instance 1, got nil")
	}
	if got.GameID != domain.GameValheim || got.Name != "Odin's Hall" || got.Password != "valheimpass" || got.Tier != domain.TierLarge {
		t.Fatalf("unexpected instance retrieved: %+v", got)
	}

	// 3. Update state
	if err := repo.UpdateState(1, domain.StateStopped); err != nil {
		t.Fatalf("UpdateState failed: %v", err)
	}
	got, _ = repo.Get(1)
	if got.State != domain.StateStopped {
		t.Fatalf("expected stopped, got %s", got.State)
	}

	// 4. List via repo
	list, err := repo.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 1 || list[0].Number != 1 || list[0].GameID != domain.GameValheim {
		t.Fatalf("unexpected List output: %+v", list)
	}

	// 5. Delete via repo
	if err := repo.Delete(1); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	got, _ = repo.Get(1)
	if got != nil {
		t.Fatalf("expected nil after delete, got %+v", got)
	}
}

func TestAuditEventsAndHistory(t *testing.T) {
	s := newTestStore(t)

	// Record an audit action
	if err := s.RecordAudit("user@example.com", "restart", "manual restart"); err != nil {
		t.Fatalf("RecordAudit failed: %v", err)
	}

	// Record an event
	if err := s.RecordEvent("join", "Thor joined the game"); err != nil {
		t.Fatalf("RecordEvent failed: %v", err)
	}

	// Record an excluded event (restart) that ListHistory should filter out
	if err := s.RecordEvent("restart", "container restarted"); err != nil {
		t.Fatalf("RecordEvent excluded failed: %v", err)
	}

	history, err := s.ListHistory(10)
	if err != nil {
		t.Fatalf("ListHistory failed: %v", err)
	}

	// Should have 2 entries: 1 audit (action) and 1 event (join).
	// The event 'restart' must be filtered out.
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}

	kinds := map[string]bool{}
	for _, h := range history {
		kinds[h.Kind] = true
	}
	if !kinds["restart"] || !kinds["join"] {
		t.Fatalf("expected restart action and join event, got %+v", history)
	}
}

func TestModIndexAndReadme(t *testing.T) {
	s := newTestStore(t)

	// Readme miss
	markdown, hit, err := s.GetReadme("author/mod", "1.0.0")
	if err != nil {
		t.Fatalf("GetReadme miss error: %v", err)
	}
	if hit || markdown != "" {
		t.Fatalf("expected cache miss, got hit=%v markdown=%q", hit, markdown)
	}

	// Readme put and hit
	if err := s.PutReadme("author/mod", "1.0.0", "# Mod Documentation"); err != nil {
		t.Fatalf("PutReadme failed: %v", err)
	}
	markdown, hit, err = s.GetReadme("author/mod", "1.0.0")
	if err != nil {
		t.Fatalf("GetReadme hit error: %v", err)
	}
	if !hit || markdown != "# Mod Documentation" {
		t.Fatalf("expected cache hit with markdown, got hit=%v markdown=%q", hit, markdown)
	}

	// Mod index save and load
	now := time.Now().Truncate(time.Second)
	rows := []ModIndexRow{
		{
			FullName:     "author/popular",
			Namespace:    "author",
			Name:         "popular",
			Owner:        "author",
			Version:      "2.0.0",
			Description:  "Very popular mod",
			Downloads:    1000,
			IsDeprecated: false,
			UpdatedAt:    now,
		},
		{
			FullName:     "author/obscure",
			Namespace:    "author",
			Name:         "obscure",
			Owner:        "author",
			Version:      "0.1.0",
			Description:  "Less popular mod",
			Downloads:    5,
			IsDeprecated: true,
			UpdatedAt:    now,
		},
	}

	if err := s.SaveModIndex(rows, now); err != nil {
		t.Fatalf("SaveModIndex failed: %v", err)
	}

	loaded, fetchedAt, err := s.LoadModIndex()
	if err != nil {
		t.Fatalf("LoadModIndex failed: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 mods loaded, got %d", len(loaded))
	}
	if loaded[0].FullName != "author/popular" || loaded[1].FullName != "author/obscure" {
		t.Fatalf("expected ordered by downloads DESC: %+v", loaded)
	}
	if !loaded[1].IsDeprecated {
		t.Fatalf("expected obscure mod to be marked deprecated")
	}
	if fetchedAt.IsZero() {
		t.Fatalf("expected non-zero fetchedAt time")
	}
}

func TestUserStoreCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 1. Get non-existent user
	u, hash, err := s.GetUser(ctx, "ghost")
	if err != nil {
		t.Fatalf("GetUser error: %v", err)
	}
	if u != nil || hash != "" {
		t.Fatalf("expected nil user for ghost, got %+v", u)
	}

	// 2. Create user
	now := time.Now().Truncate(time.Second)
	user := ports.User{
		Username:  "yaya",
		Email:     "yaya@example.com",
		CreatedAt: now,
	}
	if err := s.CreateUser(ctx, user, "$argon2id$mockhash1"); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// 3. Get created user
	got, gotHash, err := s.GetUser(ctx, "yaya")
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if got == nil || got.Username != "yaya" || got.Email != "yaya@example.com" || gotHash != "$argon2id$mockhash1" {
		t.Fatalf("GetUser unexpected: user=%+v hash=%s", got, gotHash)
	}

	// 4. Create second user
	user2 := ports.User{
		Username:  "admin",
		Email:     "admin@example.com",
		CreatedAt: now,
	}
	if err := s.CreateUser(ctx, user2, "$argon2id$mockhash2"); err != nil {
		t.Fatalf("CreateUser 2 failed: %v", err)
	}

	// 5. List users (alphabetical)
	list, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}
	if len(list) != 2 || list[0].Username != "admin" || list[1].Username != "yaya" {
		t.Fatalf("ListUsers unexpected order or count: %+v", list)
	}

	// 6. Delete user
	if err := s.DeleteUser(ctx, "admin"); err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}
	afterDelete, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers after delete failed: %v", err)
	}
	if len(afterDelete) != 1 || afterDelete[0].Username != "yaya" {
		t.Fatalf("expected 1 user left, got %+v", afterDelete)
	}
}

func TestModIndexConversion(t *testing.T) {
	res := []domain.ModSearchResult{
		{Owner: "author", Name: "coolmod", Version: "1.0.0", Description: "A cool mod"},
	}
	rows := ResultsToRows(res)
	if len(rows) != 1 || rows[0].Name != "coolmod" || rows[0].Namespace != "author" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	back := RowsToResults(rows)
	if len(back) != 1 || back[0].Name != "coolmod" || back[0].Owner != "author" {
		t.Fatalf("unexpected back conversion: %+v", back)
	}
}

func TestReadmeCache(t *testing.T) {
	s := newTestStore(t)

	// Cache miss
	content, hit, err := s.GetReadme("author/mod", "1.0.0")
	if err != nil {
		t.Fatalf("GetReadme error: %v", err)
	}
	if hit || content != "" {
		t.Fatalf("expected cache miss, got hit=%v, content=%q", hit, content)
	}

	// Cache put
	if err := s.PutReadme("author/mod", "1.0.0", "# Mod Title"); err != nil {
		t.Fatalf("PutReadme error: %v", err)
	}

	// Cache hit
	content, hit, err = s.GetReadme("author/mod", "1.0.0")
	if err != nil {
		t.Fatalf("GetReadme error: %v", err)
	}
	if !hit || content != "# Mod Title" {
		t.Fatalf("expected cache hit with markdown, got hit=%v, content=%q", hit, content)
	}
}

func TestHistoryAndAsTime(t *testing.T) {
	s := newTestStore(t)

	// Add audit and event records
	if _, err := s.db.Exec(`INSERT INTO audit (at, actor, action, detail) VALUES (CURRENT_TIMESTAMP, 'admin@example.com', 'restart', 'restarted server')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO events (at, kind, detail) VALUES (CURRENT_TIMESTAMP, 'custom-event', 'event detail')`); err != nil {
		t.Fatal(err)
	}

	history, err := s.ListHistory(10)
	if err != nil {
		t.Fatalf("ListHistory error: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d: %+v", len(history), history)
	}

	// Test asTime parsing variants
	now := time.Now().UTC().Truncate(time.Second)
	if parsed := asTime(now); !parsed.Equal(now) {
		t.Errorf("asTime(time.Time) failed: %v", parsed)
	}
	if parsed := asTime([]byte(now.Format(time.RFC3339))); !parsed.Equal(now) {
		t.Errorf("asTime([]byte) failed: %v", parsed)
	}
	if parsed := asTime(now.Format("2006-01-02 15:04:05.999999999-07:00")); parsed.IsZero() {
		t.Errorf("asTime nano failed")
	}
	if parsed := asTime(now.Format("2006-01-02 15:04:05")); parsed.IsZero() {
		t.Errorf("asTime standard failed")
	}
	if parsed := asTime("invalid-date-string"); !parsed.IsZero() {
		t.Errorf("asTime invalid string should be zero")
	}
	if parsed := asTime(12345); !parsed.IsZero() {
		t.Errorf("asTime unsupported type should be zero")
	}
}

func TestStore_OpenFailure(t *testing.T) {
	// Attempt opening SQLite in a nonexistent directory
	_, err := Open("/dev/null/invalid_dir/db.sqlite")
	if err == nil {
		t.Fatalf("expected Open failure on invalid path, got nil")
	}
}

func TestStore_ClosedStoreErrors(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "closed.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	repo := NewInstanceRepo(s)
	vhRepo := NewValheimInstanceRepo(s)

	if err := s.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// 1. History
	if _, err := s.ListHistory(10); err == nil {
		t.Error("expected error from ListHistory on closed store")
	}

	// 2. Incidents
	if _, err := s.RecordIncident(ctx, domain.Incident{GameID: domain.GameMinecraft, Number: 1, Reason: "oom"}); err == nil {
		t.Error("expected error from RecordIncident on closed store")
	}
	if _, err := s.LastIncident(ctx, domain.GameMinecraft, 1); err == nil {
		t.Error("expected error from LastIncident on closed store")
	}
	if _, err := s.ListIncidents(ctx, domain.GameMinecraft, 1, 10); err == nil {
		t.Error("expected error from ListIncidents on closed store")
	}

	// 3. Instances (MC)
	if _, err := s.GetInstance(1); err == nil {
		t.Error("expected error from GetInstance on closed store")
	}
	if _, err := s.ListInstances(); err == nil {
		t.Error("expected error from ListInstances on closed store")
	}

	// 4. Instances (Valheim)
	if _, err := s.GetValheimInstance(1); err == nil {
		t.Error("expected error from GetValheimInstance on closed store")
	}
	if _, err := s.ListValheimInstances(); err == nil {
		t.Error("expected error from ListValheimInstances on closed store")
	}
	if _, err := s.ValheimInstancesMissingSource(); err == nil {
		t.Error("expected error from ValheimInstancesMissingSource on closed store")
	}

	// 5. Mods
	if err := s.SaveModIndex([]ModIndexRow{{FullName: "author-mod"}}, time.Now()); err == nil {
		t.Error("expected error from SaveModIndex on closed store")
	}
	if _, _, err := s.LoadModIndex(); err == nil {
		t.Error("expected error from LoadModIndex on closed store")
	}
	if _, _, err := s.GetReadme("author-mod", "1.0.0"); err == nil {
		t.Error("expected error from GetReadme on closed store")
	}

	// 6. Players
	if _, err := s.ListPlayers(); err == nil {
		t.Error("expected error from ListPlayers on closed store")
	}

	// 9. ModIndex errors on missing table
	sTable := newTestStore(t)
	if _, err := sTable.db.Exec("DROP TABLE mod_index"); err != nil {
		t.Fatal(err)
	}
	if err := sTable.SaveModIndex([]ModIndexRow{{FullName: "mod"}}, time.Now()); err == nil {
		t.Error("expected SaveModIndex error when mod_index table is dropped")
	}
	if _, _, err := sTable.LoadModIndex(); err == nil {
		t.Error("expected LoadModIndex error when mod_index table is dropped")
	}

	// 7. Users
	if err := s.CreateUser(ctx, ports.User{Username: "user"}, "hash"); err == nil {
		t.Error("expected error from CreateUser on closed store")
	}
	if _, err := s.ListUsers(ctx); err == nil {
		t.Error("expected error from ListUsers on closed store")
	}
	if err := s.DeleteUser(ctx, "user"); err == nil {
		t.Error("expected error from DeleteUser on closed store")
	}

	// 8. Instance Repositories
	if _, err := repo.List(); err == nil {
		t.Error("expected error from repo.List on closed store")
	}
	if _, err := repo.Get(1); err == nil {
		t.Error("expected error from repo.Get on closed store")
	}
	if _, err := vhRepo.List(); err == nil {
		t.Error("expected error from vhRepo.List on closed store")
	}
	if _, err := vhRepo.Get(1); err == nil {
		t.Error("expected error from vhRepo.Get on closed store")
	}
}

