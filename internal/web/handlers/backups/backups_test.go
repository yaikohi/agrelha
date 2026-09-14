package backups

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"

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

type mockDirEntry struct {
	name  string
	isDir bool
	err   error
}

func (m mockDirEntry) Name() string               { return m.name }
func (m mockDirEntry) IsDir() bool                { return m.isDir }
func (m mockDirEntry) Type() os.FileMode          { return 0 }
func (m mockDirEntry) Info() (os.FileInfo, error) { return nil, m.err }

func TestBackupsEdges(t *testing.T) {
	// 1. Actor fallback
	hActor := New(Config{})
	app := fiber.New()
	var fastCtx fasthttp.RequestCtx
	c := app.AcquireCtx(&fastCtx)
	defer app.ReleaseCtx(c)

	if a := hActor.cfg.Actor(c); a != "-" {
		t.Errorf("expected '-', got %q", a)
	}
	c.Locals("actor", "admin")
	if a := hActor.cfg.Actor(c); a != "admin" {
		t.Errorf("expected 'admin', got %q", a)
	}

	// 2. BackupInfo edge cases
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	mcRepo := store.NewInstanceRepo(st)
	_ = mcRepo.Upsert(domain.Instance{
		Number: 1, Name: "MC", State: domain.StateRunning, GameID: domain.GameMinecraft,
	})
	mcMgr := instances.NewInstanceManager(mcRepo, nil, nil, 32, 8, 4, "manifests/minecraft-modded", "192.168.20.225", nil, "minecraft-modded", instances.WithBackupsDir(t.TempDir()))
	hMC := New(Config{MCInstances: mcMgr})
	if _, ok := hMC.BackupInfo(); !ok {
		t.Errorf("expected BackupInfo ok = true with MCInstances")
	}

	origReadDir := readDir
	defer func() { readDir = origReadDir }()
	readDir = func(string) ([]os.DirEntry, error) {
		return nil, errors.New("read error")
	}
	hErr := New(Config{BackupsDir: "/some/dir"})
	if _, ok := hErr.BackupInfo(); ok {
		t.Errorf("expected BackupInfo ok = false on readDir error")
	}

	readDir = func(string) ([]os.DirEntry, error) {
		return []os.DirEntry{mockDirEntry{name: "broken", isDir: false, err: errors.New("info err")}}, nil
	}
	if sum, ok := hErr.BackupInfo(); !ok || sum.Count != 0 {
		t.Errorf("expected count = 0 on broken entry info, got %v, ok=%v", sum, ok)
	}

	// 3. Routing edge cases
	appRoutes := fiber.New()
	hRoutes := New(Config{
		MCInstances: mcMgr,
	})
	appRoutes.Post("/test/mc/:num/create", hRoutes.Create)
	appRoutes.Post("/test/mc/:num/restore-inplace", hRoutes.RestoreInPlace)
	appRoutes.Post("/test/mc/:num/restore-new", hRoutes.RestoreNew)

	// mc create invalid num
	resp, _ := appRoutes.Test(httptest.NewRequest(http.MethodPost, "/test/mc/abc/create", nil))
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Invalid instance number") {
		t.Errorf("expected invalid instance number: %s", string(body))
	}

	// mc restore-inplace unconfigured
	hUnconf := New(Config{})
	appUnconf := fiber.New()
	appUnconf.Post("/test/mc/:num/restore-inplace", hUnconf.RestoreInPlace)
	resp, _ = appUnconf.Test(httptest.NewRequest(http.MethodPost, "/test/mc/1/restore-inplace", nil))
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Instance manager unconfigured") {
		t.Errorf("expected unconfigured: %s", string(body))
	}

	// mc restore-inplace invalid num
	resp, _ = appRoutes.Test(httptest.NewRequest(http.MethodPost, "/test/mc/abc/restore-inplace", nil))
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Invalid instance number") {
		t.Errorf("expected invalid instance number: %s", string(body))
	}

	// mc restore-new unconfigured
	appUnconf.Post("/test/mc/:num/restore-new", hUnconf.RestoreNew)
	resp, _ = appUnconf.Test(httptest.NewRequest(http.MethodPost, "/test/mc/1/restore-new", nil))
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Instance manager unconfigured") {
		t.Errorf("expected unconfigured: %s", string(body))
	}

	// mc restore-new invalid num
	resp, _ = appRoutes.Test(httptest.NewRequest(http.MethodPost, "/test/mc/abc/restore-new", nil))
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Invalid instance number") {
		t.Errorf("expected invalid instance number: %s", string(body))
	}

	// mc restore-new manager error
	reqNewErr := httptest.NewRequest(http.MethodPost, "/test/mc/999/restore-new", strings.NewReader(`{"name":"World","tier":"medium","archive":"arch.tar.gz"}`))
	reqNewErr.Header.Set("Content-Type", "application/json")
	resp, _ = appRoutes.Test(reqNewErr)
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "not found") {
		t.Errorf("expected error from restore-new: %s", string(body))
	}

	// mc restore-inplace archive from form value and query
	reqForm := httptest.NewRequest(http.MethodPost, "/test/mc/1/restore-inplace", strings.NewReader("archive=arch.tar.gz"))
	reqForm.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, _ = appRoutes.Test(reqForm)

	reqQuery := httptest.NewRequest(http.MethodPost, "/test/mc/1/restore-inplace?archive=arch.tar.gz", nil)
	_, _ = appRoutes.Test(reqQuery)

	reqNoArchive := httptest.NewRequest(http.MethodPost, "/test/mc/1/restore-inplace", nil)
	_, _ = appRoutes.Test(reqNoArchive)

	// 4. MC Delete edges
	hDelMC := New(Config{MCInstances: mcMgr})
	appDelMC := fiber.New()
	appDelMC.Post("/api/minecraft/:num/backups/delete", hDelMC.Delete)
	reqDelErr := httptest.NewRequest(http.MethodPost, "/api/minecraft/1/backups/delete", strings.NewReader(`{"file":"missing.tar.gz"}`))
	reqDelErr.Header.Set("Content-Type", "application/json")
	resp, _ = appDelMC.Test(reqDelErr)
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "invalid or unauthorized backup file name") && !strings.Contains(string(body), "not found") {
		t.Errorf("expected delete error: %s", string(body))
	}

	dir := t.TempDir()
	hDirOnly := New(Config{BackupsDir: dir})
	appDirOnly := fiber.New()
	appDirOnly.Post("/api/minecraft/:num/backups/delete", hDirOnly.Delete)
	reqUnsafe := httptest.NewRequest(http.MethodPost, "/api/minecraft/1/backups/delete", strings.NewReader(`{"file":"../unsafe.tar.gz"}`))
	reqUnsafe.Header.Set("Content-Type", "application/json")
	resp, _ = appDirOnly.Test(reqUnsafe)
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "invalid backup file name") {
		t.Errorf("expected invalid backup file name: %s", string(body))
	}

	reqMissingFile := httptest.NewRequest(http.MethodPost, "/api/minecraft/1/backups/delete", strings.NewReader(`{"file":"mc-backup-01-20260901-120000.tar.gz"}`))
	reqMissingFile.Header.Set("Content-Type", "application/json")
	resp, _ = appDirOnly.Test(reqMissingFile)
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Failed to delete backup") {
		t.Errorf("expected Failed to delete backup: %s", string(body))
	}

	appUnconf.Post("/api/minecraft/:num/backups/delete", hUnconf.Delete)
	reqUnconfDel := httptest.NewRequest(http.MethodPost, "/api/minecraft/1/backups/delete", strings.NewReader(`{"file":"safe.tar.gz"}`))
	reqUnconfDel.Header.Set("Content-Type", "application/json")
	resp, _ = appUnconf.Test(reqUnconfDel)
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Backups directory unconfigured") {
		t.Errorf("expected Backups directory unconfigured: %s", string(body))
	}

	// 5. Valheim edges
	vhRepo := store.NewValheimInstanceRepo(st)
	_ = vhRepo.Upsert(domain.Instance{
		Number: 1, Name: "Valheim", State: domain.StateRunning, GameID: domain.GameValheim,
	})
	vhMgr := instances.NewInstanceManager(vhRepo, nil, nil, 16, 4, 2, "manifests/valheim", "192.168.20.224", nil, "valheim", instances.WithGameID(domain.GameValheim))
	hVH := New(Config{ValheimInstances: vhMgr})
	appVH := fiber.New()
	appVH.Post("/test/vh/:num/create", hVH.ValheimCreate)
	appVH.Post("/test/vh/:num/restore-inplace", hVH.ValheimRestoreInPlace)
	appVH.Post("/test/vh/:num/restore-new", hVH.ValheimRestoreNew)
	appVH.Post("/test/vh/:num/delete", hVH.ValheimDelete)

	appUnconf.Post("/test/vh/:num/create", hUnconf.ValheimCreate)
	resp, _ = appUnconf.Test(httptest.NewRequest(http.MethodPost, "/test/vh/1/create", nil))
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Valheim instance manager unconfigured") {
		t.Errorf("expected unconfigured: %s", string(body))
	}

	resp, _ = appVH.Test(httptest.NewRequest(http.MethodPost, "/test/vh/abc/create", nil))
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Invalid instance number") {
		t.Errorf("expected invalid num: %s", string(body))
	}

	resp, _ = appVH.Test(httptest.NewRequest(http.MethodPost, "/test/vh/999/create", nil))
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "not found") {
		t.Errorf("expected not found: %s", string(body))
	}

	appUnconf.Post("/test/vh/:num/restore-inplace", hUnconf.ValheimRestoreInPlace)
	resp, _ = appUnconf.Test(httptest.NewRequest(http.MethodPost, "/test/vh/1/restore-inplace", nil))
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Valheim instance manager unconfigured") {
		t.Errorf("expected unconfigured: %s", string(body))
	}

	resp, _ = appVH.Test(httptest.NewRequest(http.MethodPost, "/test/vh/abc/restore-inplace", nil))
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Invalid instance number") {
		t.Errorf("expected invalid num: %s", string(body))
	}

	reqFormVH := httptest.NewRequest(http.MethodPost, "/test/vh/1/restore-inplace", strings.NewReader("archive=arch.tar.gz"))
	reqFormVH.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, _ = appVH.Test(reqFormVH)

	reqQueryVH := httptest.NewRequest(http.MethodPost, "/test/vh/1/restore-inplace?archive=arch.tar.gz", nil)
	_, _ = appVH.Test(reqQueryVH)

	reqNoArchiveVH := httptest.NewRequest(http.MethodPost, "/test/vh/1/restore-inplace", nil)
	_, _ = appVH.Test(reqNoArchiveVH)

	reqErrVH := httptest.NewRequest(http.MethodPost, "/test/vh/999/restore-inplace", strings.NewReader(`{"archive":"arch.tar.gz"}`))
	reqErrVH.Header.Set("Content-Type", "application/json")
	resp, _ = appVH.Test(reqErrVH)
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "not found") {
		t.Errorf("expected not found: %s", string(body))
	}

	appUnconf.Post("/test/vh/:num/restore-new", hUnconf.ValheimRestoreNew)
	resp, _ = appUnconf.Test(httptest.NewRequest(http.MethodPost, "/test/vh/1/restore-new", nil))
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Valheim instance manager unconfigured") {
		t.Errorf("expected unconfigured: %s", string(body))
	}

	resp, _ = appVH.Test(httptest.NewRequest(http.MethodPost, "/test/vh/abc/restore-new", nil))
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Invalid instance number") {
		t.Errorf("expected invalid num: %s", string(body))
	}

	reqNewVHErr := httptest.NewRequest(http.MethodPost, "/test/vh/999/restore-new", strings.NewReader(`{"name":"VNew","tier":"small","archive":"arch.tar.gz"}`))
	reqNewVHErr.Header.Set("Content-Type", "application/json")
	resp, _ = appVH.Test(reqNewVHErr)
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "not found") {
		t.Errorf("expected not found: %s", string(body))
	}

	reqDelForm := httptest.NewRequest(http.MethodPost, "/test/vh/1/delete", strings.NewReader("file=arch.tar.gz"))
	reqDelForm.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, _ = appVH.Test(reqDelForm)

	reqDelQuery := httptest.NewRequest(http.MethodPost, "/test/vh/1/delete?file=arch.tar.gz", nil)
	_, _ = appVH.Test(reqDelQuery)

	reqEmptyVH := httptest.NewRequest(http.MethodPost, "/test/vh/1/delete", strings.NewReader(`{"file":""}`))
	reqEmptyVH.Header.Set("Content-Type", "application/json")
	resp, _ = appVH.Test(reqEmptyVH)
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "File name required") {
		t.Errorf("expected File name required: %s", string(body))
	}

	reqDelMissVH := httptest.NewRequest(http.MethodPost, "/test/vh/1/delete", strings.NewReader(`{"file":"missing.tar.gz"}`))
	reqDelMissVH.Header.Set("Content-Type", "application/json")
	resp, _ = appVH.Test(reqDelMissVH)
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "not found") && !strings.Contains(string(body), "backups directory unconfigured") {
		t.Errorf("expected not found: %s", string(body))
	}

	appDirOnly.Post("/api/valheim/:num/backups/delete", hDirOnly.ValheimDelete)
	reqVHUnsafe := httptest.NewRequest(http.MethodPost, "/api/valheim/1/backups/delete", strings.NewReader(`{"file":"../unsafe.tar.gz"}`))
	reqVHUnsafe.Header.Set("Content-Type", "application/json")
	resp, _ = appDirOnly.Test(reqVHUnsafe)
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "invalid backup file name") {
		t.Errorf("expected invalid backup file name: %s", string(body))
	}

	reqVHMiss := httptest.NewRequest(http.MethodPost, "/api/valheim/1/backups/delete", strings.NewReader(`{"file":"valheim-world-01-20260901-120000.tar.gz"}`))
	reqVHMiss.Header.Set("Content-Type", "application/json")
	resp, _ = appDirOnly.Test(reqVHMiss)
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Failed to delete backup") {
		t.Errorf("expected Failed to delete backup: %s", string(body))
	}

	appUnconf.Post("/api/valheim/:num/backups/delete", hUnconf.ValheimDelete)
	reqVHUnconf := httptest.NewRequest(http.MethodPost, "/api/valheim/1/backups/delete", strings.NewReader(`{"file":"safe.tar.gz"}`))
	reqVHUnconf.Header.Set("Content-Type", "application/json")
	resp, _ = appUnconf.Test(reqVHUnconf)
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Backups directory unconfigured") {
		t.Errorf("expected Backups directory unconfigured: %s", string(body))
	}
}


