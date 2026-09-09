package wiring_test

import (
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"agrelha/internal/domain"
	"agrelha/internal/wiring"
)

func TestMinecraftInstanceConfigsAndBackups(t *testing.T) {
	app, st, mgr, d, _ := setupTestMCServer(t)
	defer st.Close()

	inst, err := mgr.CreateInstance(context.Background(), domain.Instance{
		Name:      "Ducktopia",
		Loader:    domain.LoaderNeoForge,
		Source:    domain.SourceModpack,
		MCVersion: "1.21.1",
		Tier:      domain.TierLarge,
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	// 1. GET /minecraft/1/configs renders configs list
	req := httptest.NewRequest(fiber.MethodGet, "/minecraft/1/configs", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("GET /minecraft/1/configs status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "server.properties") {
		t.Fatalf("expected server.properties in page, got: %s", string(body))
	}

	// 2. GET /api/minecraft/1/configs/file?f=server.properties returns SSE with file content
	req = httptest.NewRequest(fiber.MethodGet, "/api/minecraft/1/configs/file?f=server.properties", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("GET /api/minecraft/1/configs/file status = %d, want 200", resp.StatusCode)
	}
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "difficulty=normal") {
		t.Fatalf("expected difficulty=normal in content, got: %s", string(body))
	}

	// 3. POST /api/minecraft/1/backups/create creates backup Job
	req = httptest.NewRequest(fiber.MethodPost, "/api/minecraft/1/backups/create", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("POST /api/minecraft/1/backups/create status = %d, want 200", resp.StatusCode)
	}

	jobs, err := d.MCK8s.Clientset().BatchV1().Jobs("minecraft-modded").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs.Items) == 0 {
		t.Fatal("expected a backup Job to be created in fake clientset")
	}
	job := jobs.Items[0]
	if !strings.HasPrefix(job.Name, "mc-bkp-ducktopia-1-") {
		t.Fatalf("job name = %s, expected prefix mc-bkp-ducktopia-1-", job.Name)
	}
	if job.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName != "mc-instance-01-data" {
		t.Fatalf("job volume claim = %s, want mc-instance-01-data", job.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName)
	}
	if job.Spec.Template.Spec.Volumes[1].PersistentVolumeClaim.ClaimName != "minecraft-modded-backups" {
		t.Fatalf("backups claim = %s, want minecraft-modded-backups", job.Spec.Template.Spec.Volumes[1].PersistentVolumeClaim.ClaimName)
	}

	_ = inst
}

func TestMinecraftRestoreEndpoints(t *testing.T) {
	app, st, mgr, d, cfg := setupTestMCServer(t)
	defer st.Close()

	backupsDir := t.TempDir()
	cfg.BackupsDir = backupsDir
	app = wiring.BuildServer(context.Background(), cfg, d)

	inst, err := mgr.CreateInstance(context.Background(), domain.Instance{
		Name:      "Fluxweave",
		Loader:    domain.LoaderNeoForge,
		Source:    domain.SourceModlist,
		MCVersion: "1.21.1",
		Tier:      domain.TierMedium,
	}, "jei\n")
	if err != nil {
		t.Fatal(err)
	}

	backupFile := "mc-fluxweave-01-test-20260101-120000.tar.gz"
	backupPath := filepath.Join(backupsDir, backupFile)
	if err := os.WriteFile(backupPath, []byte("dummy tar data"), 0644); err != nil {
		t.Fatal(err)
	}

	// 1. In-place restore should fail when instance is running
	_ = st.UpdateInstanceState(inst.Number, string(domain.StateRunning))
	req := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/1/backups/restore-inplace", strings.NewReader(`{"archive":"`+backupFile+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Cannot restore while world is running") {
		t.Fatalf("expected running error toast, got: %s", string(body))
	}

	// 2. In-place restore should succeed when instance is stopped
	_ = st.UpdateInstanceState(inst.Number, string(domain.StateStopped))
	req = httptest.NewRequest(fiber.MethodPost, "/api/minecraft/1/backups/restore-inplace", strings.NewReader(`{"archive":"`+backupFile+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "In-place restore started") {
		t.Fatalf("expected restore started toast, got: %s", string(body))
	}

	// 3. Restore as New World creates slot #02 and launches restore Job
	req = httptest.NewRequest(fiber.MethodPost, "/api/minecraft/1/backups/restore-new", strings.NewReader(`{"name":"Fluxweave Clone","tier":"large","archive":"`+backupFile+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Fluxweave Clone") {
		t.Fatalf("expected clone created toast, got: %s", string(body))
	}

	newInst, err := mgr.GetInstance(context.Background(), 2)
	if err != nil || newInst == nil {
		t.Fatal("expected instance #02 to exist after restore-new")
	}
	if newInst.Tier != domain.TierLarge {
		t.Errorf("new instance tier = %s, want large", newInst.Tier)
	}

	// 4. Download backup file
	req = httptest.NewRequest(fiber.MethodGet, "/api/minecraft/1/backups/download?f="+backupFile, nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("download status = %d, want 200", resp.StatusCode)
	}
	dlData, _ := io.ReadAll(resp.Body)
	if string(dlData) != "dummy tar data" {
		t.Fatalf("download content = %q, want 'dummy tar data'", string(dlData))
	}

	// 5. Delete backup file
	req = httptest.NewRequest(fiber.MethodPost, "/api/minecraft/1/backups/delete", strings.NewReader(`{"file":"`+backupFile+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Errorf("expected %s to be deleted from disk", backupPath)
	}
}
