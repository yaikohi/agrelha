package backups

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
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
