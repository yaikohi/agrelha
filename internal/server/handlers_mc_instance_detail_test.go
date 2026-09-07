package server

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"agrelha/internal/minecraft"
)

func TestMinecraftInstanceConfigsAndBackups(t *testing.T) {
	s, st, mgr := setupTestMCServer(t)
	defer st.Close()

	inst, err := mgr.CreateInstance(context.Background(), minecraft.Instance{
		Name:      "Ducktopia",
		Loader:    minecraft.LoaderNeoForge,
		Source:    minecraft.SourceModpack,
		MCVersion: "1.21.1",
		Tier:      minecraft.TierLarge,
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	// 1. GET /minecraft/1/configs renders configs list
	req := httptest.NewRequest(fiber.MethodGet, "/minecraft/1/configs", nil)
	resp, err := s.App.Test(req)
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
	resp, err = s.App.Test(req)
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
	resp, err = s.App.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("POST /api/minecraft/1/backups/create status = %d, want 200", resp.StatusCode)
	}

	jobs, err := s.mck8s.Clientset().BatchV1().Jobs("minecraft-modded").List(context.Background(), metav1.ListOptions{})
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
