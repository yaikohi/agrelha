package backups

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"agrelha/internal/infra/manifests"
	valheimmanifests "agrelha/internal/infra/manifests/valheim"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
)

func TestBackupsDownload(t *testing.T) {
	dir := t.TempDir()
	archiveName := "mc-world-01-20260909-120000.tar.gz"
	archivePath := filepath.Join(dir, archiveName)
	if err := os.WriteFile(archivePath, []byte("fake-tar-gz-content"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(Config{BackupsDir: dir})
	app := fiber.New()
	h.Register(app)

	// 1. Success download
	req := httptest.NewRequest(http.MethodGet, "/api/minecraft/1/backups/download?f="+archiveName, nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "fake-tar-gz-content" {
		t.Errorf("download body = %q, want fake-tar-gz-content", string(body))
	}

	// 2. 404 for missing file
	req404 := httptest.NewRequest(http.MethodGet, "/api/minecraft/1/backups/download?f=missing.tar.gz", nil)
	resp404, _ := app.Test(req404)
	if resp404.StatusCode != http.StatusNotFound {
		t.Errorf("missing file status = %d, want 404", resp404.StatusCode)
	}

	// 3. 400 for bad file name (path traversal attempt)
	req400 := httptest.NewRequest(http.MethodGet, "/api/minecraft/1/backups/download?f=badname.txt", nil)
	resp400, _ := app.Test(req400)
	if resp400.StatusCode != http.StatusBadRequest {
		t.Errorf("bad name status = %d, want 400", resp400.StatusCode)
	}
}

func TestBackupsDelete(t *testing.T) {
	dir := t.TempDir()
	archiveName := "mc-world-01-20260909-120000.tar.gz"
	archivePath := filepath.Join(dir, archiveName)
	if err := os.WriteFile(archivePath, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(Config{BackupsDir: dir})
	app := fiber.New()
	h.Register(app)

	// 1. Delete file
	req := httptest.NewRequest(http.MethodPost, "/api/minecraft/1/backups/delete", strings.NewReader(`{"file":"`+archiveName+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Deleted "+archiveName) {
		t.Errorf("delete response = %s, expected Deleted message", string(body))
	}
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Errorf("file was not deleted from disk")
	}

	// 2. Delete with empty filename returns error toast
	reqEmpty := httptest.NewRequest(http.MethodPost, "/api/minecraft/1/backups/delete", strings.NewReader(`{"file":""}`))
	reqEmpty.Header.Set("Content-Type", "application/json")
	respEmpty, _ := app.Test(reqEmpty)
	bodyEmpty, _ := io.ReadAll(respEmpty.Body)
	if !strings.Contains(string(bodyEmpty), "File name required") {
		t.Errorf("empty delete response = %s, expected File name required", string(bodyEmpty))
	}
}

func TestBackupsUnconfigured(t *testing.T) {
	h := New(Config{})
	app := fiber.New()
	h.Register(app)

	// 1. Create returns unconfigured error
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/minecraft/1/backups/create", nil)
	respCreate, err := app.Test(reqCreate)
	if err != nil {
		t.Fatal(err)
	}
	bodyCreate, _ := io.ReadAll(respCreate.Body)
	if !strings.Contains(string(bodyCreate), "Instance manager unconfigured") {
		t.Errorf("create response = %s", string(bodyCreate))
	}

	// 2. Download returns 503
	reqDl := httptest.NewRequest(http.MethodGet, "/api/minecraft/1/backups/download?f=file.tar.gz", nil)
	respDl, _ := app.Test(reqDl)
	if respDl.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("dl status = %d, want 503", respDl.StatusCode)
	}

	// 3. BackupInfo handles empty dir gracefully
	_, ok := h.BackupInfo()
	if ok {
		t.Errorf("expected BackupInfo to report not ok on empty dir")
	}
}

func TestValheimBackups(t *testing.T) {
	dir := t.TempDir()
	archiveName := "valheim-world-01-20260909-120000.tar.gz"
	archivePath := filepath.Join(dir, archiveName)
	if err := os.WriteFile(archivePath, []byte("fake-valheim-tar-gz"), 0644); err != nil {
		t.Fatal(err)
	}

	h := New(Config{BackupsDir: dir})
	app := fiber.New()
	h.Register(app)

	// 1. Download
	reqDl := httptest.NewRequest(http.MethodGet, "/api/valheim/1/backups/download?f="+archiveName, nil)
	respDl, err := app.Test(reqDl)
	if err != nil {
		t.Fatal(err)
	}
	if respDl.StatusCode != http.StatusOK {
		t.Fatalf("valheim download status = %d, want 200", respDl.StatusCode)
	}
	body, _ := io.ReadAll(respDl.Body)
	if string(body) != "fake-valheim-tar-gz" {
		t.Errorf("valheim download body = %q, want fake-valheim-tar-gz", string(body))
	}

	// 2. Delete
	reqDel := httptest.NewRequest(http.MethodPost, "/api/valheim/1/backups/delete", strings.NewReader(`{"file":"`+archiveName+`"}`))
	reqDel.Header.Set("Content-Type", "application/json")
	respDel, err := app.Test(reqDel)
	if err != nil {
		t.Fatal(err)
	}
	if respDel.StatusCode != http.StatusOK {
		t.Fatalf("valheim delete status = %d, want 200", respDel.StatusCode)
	}
	delBody, _ := io.ReadAll(respDel.Body)
	if !strings.Contains(string(delBody), "Deleted "+archiveName) {
		t.Errorf("valheim delete response = %s, expected Deleted message", string(delBody))
	}
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Errorf("file was not deleted from disk")
	}
}

type mockJobRunner struct {
	backupErr  error
	restoreErr error
}

func (m *mockJobRunner) CreateBackupJob(ctx context.Context, jobName, backupName, dataPVC, backupsPVC string) error {
	return m.backupErr
}

func (m *mockJobRunner) CreateRestoreJob(ctx context.Context, jobName, archiveName, dataPVC, backupsPVC string) error {
	return m.restoreErr
}

type mockStateStore struct {
	docs map[string]ports.Document
}

func (s *mockStateStore) Get(_ context.Context, path string) (ports.Document, error) {
	if s.docs == nil {
		return ports.Document{Data: make(map[string]string)}, nil
	}
	return s.docs[path], nil
}
func (s *mockStateStore) Put(_ context.Context, path string, doc ports.Document, _ string) error {
	if s.docs == nil {
		s.docs = make(map[string]ports.Document)
	}
	s.docs[path] = doc
	return nil
}
func (s *mockStateStore) Patch(ctx context.Context, path, msg string, fn func(*ports.Document) (bool, error)) (bool, error) {
	if s.docs == nil {
		s.docs = make(map[string]ports.Document)
	}
	doc := s.docs[path]
	changed, err := fn(&doc)
	if err != nil {
		return false, err
	}
	if changed {
		s.docs[path] = doc
	}
	return changed, nil
}
func (s *mockStateStore) Delete(_ context.Context, path, _ string) error {
	delete(s.docs, path)
	return nil
}
func (s *mockStateStore) PutTree(_ context.Context, _ string, tree map[string]ports.Document, _ string) error {
	if s.docs == nil {
		s.docs = make(map[string]ports.Document)
	}
	for k, v := range tree {
		s.docs[k] = v
	}
	return nil
}

func TestMCBackupsEndpoints(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	backupsDir := t.TempDir()
	archiveName := "mc-ducktopia-01-20260909120000.tar.gz"
	_ = os.WriteFile(filepath.Join(backupsDir, archiveName), []byte("archive-content"), 0644)

	repo := store.NewInstanceRepo(st)
	_ = repo.Upsert(domain.Instance{
		Number:    1,
		Slug:      "ducktopia",
		Name:      "Ducktopia",
		State:     domain.StateStopped,
		GameID:    domain.GameMinecraft,
		MCVersion: "1.21.1",
		Loader:    domain.LoaderNeoForge,
		Tier:      domain.TierMedium,
	})

	stateStore := &mockStateStore{}
	jobRunner := &mockJobRunner{}
	mcMgr := instances.NewInstanceManager(
		repo, stateStore, nil, 32, 8, 4,
		"manifests/minecraft-modded", "192.168.20.224",
		manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded"),
		"minecraft-modded",
		instances.WithGameID(domain.GameMinecraft),
		instances.WithBackupsDir(backupsDir),
		instances.WithJobRunner(jobRunner),
	)

	h := New(Config{
		BackupsDir:  backupsDir,
		MCInstances: mcMgr,
	})

	app := fiber.New()
	h.Register(app)

	// 1. Create backup
	reqCreate := httptest.NewRequest("POST", "/api/minecraft/1/backups/create", nil)
	respCreate, err := app.Test(reqCreate)
	if err != nil || respCreate.StatusCode != fiber.StatusOK {
		t.Fatalf("create backup failed: %v, status: %d", err, respCreate.StatusCode)
	}
	bodyCreate, _ := io.ReadAll(respCreate.Body)
	if !strings.Contains(string(bodyCreate), "Backup job started") {
		t.Errorf("expected Backup job started message, got: %s", string(bodyCreate))
	}

	// 2. Restore in-place
	reqRestore := httptest.NewRequest("POST", "/api/minecraft/1/backups/restore-inplace", strings.NewReader(`{"archive":"`+archiveName+`"}`))
	reqRestore.Header.Set("Content-Type", "application/json")
	respRestore, err := app.Test(reqRestore)
	if err != nil || respRestore.StatusCode != fiber.StatusOK {
		t.Fatalf("restore in place failed: %v, status: %d", err, respRestore.StatusCode)
	}
	bodyRestore, _ := io.ReadAll(respRestore.Body)
	if !strings.Contains(string(bodyRestore), "In-place restore started") {
		t.Errorf("expected In-place restore started message, got: %s", string(bodyRestore))
	}

	// 3. Restore new
	reqNew := httptest.NewRequest("POST", "/api/minecraft/1/backups/restore-new", strings.NewReader(`{"name":"New Duck","tier":"small","archive":"`+archiveName+`"}`))
	reqNew.Header.Set("Content-Type", "application/json")
	respNew, err := app.Test(reqNew)
	if err != nil || respNew.StatusCode != fiber.StatusOK {
		t.Fatalf("restore new failed: %v, status: %d", err, respNew.StatusCode)
	}
	bodyNew, _ := io.ReadAll(respNew.Body)
	if !strings.Contains(string(bodyNew), "created from backup") {
		t.Errorf("expected created from backup message, got: %s", string(bodyNew))
	}

	// 4. Delete backup via MCInstances
	reqDel := httptest.NewRequest("POST", "/api/minecraft/1/backups/delete", strings.NewReader(`{"file":"`+archiveName+`"}`))
	reqDel.Header.Set("Content-Type", "application/json")
	respDel, err := app.Test(reqDel)
	if err != nil || respDel.StatusCode != fiber.StatusOK {
		t.Fatalf("delete backup failed: %v, status: %d", err, respDel.StatusCode)
	}

	// 5. Error cases: invalid instance number
	reqBadNum := httptest.NewRequest("POST", "/api/minecraft/99/backups/create", nil)
	respBadNum, _ := app.Test(reqBadNum)
	bodyBadNum, _ := io.ReadAll(respBadNum.Body)
	if !strings.Contains(string(bodyBadNum), "instance not found") {
		t.Errorf("expected instance not found, got: %s", string(bodyBadNum))
	}

	reqBadRestore := httptest.NewRequest("POST", "/api/minecraft/99/backups/restore-inplace", strings.NewReader(`{"archive":"`+archiveName+`"}`))
	reqBadRestore.Header.Set("Content-Type", "application/json")
	respBadRestore, _ := app.Test(reqBadRestore)
	bodyBadRestore, _ := io.ReadAll(respBadRestore.Body)
	if !strings.Contains(string(bodyBadRestore), "instance not found") {
		t.Errorf("expected instance not found, got: %s", string(bodyBadRestore))
	}
}

func TestValheimBackupsEndpoints(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "valheim-test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	backupsDir := t.TempDir()
	archiveName := "valheim-midgard-01-20260909120000.tar.gz"
	_ = os.WriteFile(filepath.Join(backupsDir, archiveName), []byte("archive-content"), 0644)

	repo := store.NewValheimInstanceRepo(st)
	_ = repo.Upsert(domain.Instance{
		Number:   1,
		Slug:     "midgard",
		Name:     "Midgard",
		State:    domain.StateStopped,
		GameID:   domain.GameValheim,
		Password: "secretpassword",
		Tier:     domain.TierMedium,
	})

	stateStore := &mockStateStore{}
	jobRunner := &mockJobRunner{}
	valheimMgr := instances.NewInstanceManager(
		repo, stateStore, nil, 16, 4, 2,
		"manifests/valheim", "192.168.20.224",
		valheimmanifests.New("ykhi.xyz/gameserver=true", "valheim"),
		"valheim",
		instances.WithGameID(domain.GameValheim),
		instances.WithBackupsDir(backupsDir),
		instances.WithJobRunner(jobRunner),
	)

	h := New(Config{
		BackupsDir:       backupsDir,
		ValheimInstances: valheimMgr,
	})

	app := fiber.New()
	h.Register(app)

	// 1. Create backup
	reqCreate := httptest.NewRequest("POST", "/api/valheim/1/backups/create", nil)
	respCreate, err := app.Test(reqCreate)
	if err != nil || respCreate.StatusCode != fiber.StatusOK {
		t.Fatalf("valheim create backup failed: %v, status: %d", err, respCreate.StatusCode)
	}
	bodyCreate, _ := io.ReadAll(respCreate.Body)
	if !strings.Contains(string(bodyCreate), "Backup job started") {
		t.Errorf("expected Backup job started, got: %s", string(bodyCreate))
	}

	// 2. Restore in-place
	reqRestore := httptest.NewRequest("POST", "/api/valheim/1/backups/restore-inplace", strings.NewReader(`{"archive":"`+archiveName+`"}`))
	reqRestore.Header.Set("Content-Type", "application/json")
	respRestore, err := app.Test(reqRestore)
	if err != nil || respRestore.StatusCode != fiber.StatusOK {
		t.Fatalf("valheim restore in place failed: %v, status: %d", err, respRestore.StatusCode)
	}
	bodyRestore, _ := io.ReadAll(respRestore.Body)
	if !strings.Contains(string(bodyRestore), "In-place restore started") {
		t.Errorf("expected In-place restore started, got: %s", string(bodyRestore))
	}

	// 3. Restore new
	reqNew := httptest.NewRequest("POST", "/api/valheim/1/backups/restore-new", strings.NewReader(`{"name":"New Midgard","tier":"small","archive":"`+archiveName+`"}`))
	reqNew.Header.Set("Content-Type", "application/json")
	respNew, err := app.Test(reqNew)
	if err != nil || respNew.StatusCode != fiber.StatusOK {
		t.Fatalf("valheim restore new failed: %v, status: %d", err, respNew.StatusCode)
	}
	bodyNew, _ := io.ReadAll(respNew.Body)
	if !strings.Contains(string(bodyNew), "created from backup") {
		t.Errorf("expected created from backup, got: %s", string(bodyNew))
	}

	// 4. Delete backup via ValheimInstances
	reqDel := httptest.NewRequest("POST", "/api/valheim/1/backups/delete", strings.NewReader(`{"file":"`+archiveName+`"}`))
	reqDel.Header.Set("Content-Type", "application/json")
	respDel, err := app.Test(reqDel)
	if err != nil || respDel.StatusCode != fiber.StatusOK {
		t.Fatalf("valheim delete backup failed: %v, status: %d", err, respDel.StatusCode)
	}
}

func TestBackupInfoWithDirFiles(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "mc-test-01-20260901.tar.gz")
	f2 := filepath.Join(dir, "mc-test-01-20260902.tar.gz")
	_ = os.WriteFile(f1, []byte("short"), 0644)
	_ = os.WriteFile(f2, []byte("longer content"), 0644)
	_ = os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	h := New(Config{BackupsDir: dir})
	sum, ok := h.BackupInfo()
	if !ok {
		t.Fatal("expected BackupInfo ok = true")
	}
	if sum.Count != 2 {
		t.Errorf("expected 2 backups, got %d", sum.Count)
	}
}

