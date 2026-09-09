package server

import (
	"agrelha/internal/domain"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestWizardImportTxt(t *testing.T) {
	s, st, _ := setupTestMCServer(t)
	defer st.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "mods.txt")
	if err != nil {
		t.Fatal(err)
	}
	sampleTxt := "# Modpack: My Custom Pack\njei\nappleskin\nwaystones\n"
	_, _ = part.Write([]byte(sampleTxt))
	_ = writer.Close()

	req := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/wizard/import", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := s.App.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}

	if res["name"] != "My Custom Pack" {
		t.Errorf("name = %v, want 'My Custom Pack'", res["name"])
	}
	if res["loader"] != "neoforge" {
		t.Errorf("loader = %v, want 'neoforge'", res["loader"])
	}
	slugs, ok := res["slugs"].([]any)
	if !ok || len(slugs) != 3 {
		t.Fatalf("slugs count = %v, want 3", len(slugs))
	}
}

func TestLegacyRedirectsToActiveInstance(t *testing.T) {
	s, st, mgr := setupTestMCServer(t)
	defer st.Close()

	// Seed instance #01
	_, err := mgr.CreateInstance(context.Background(), domain.Instance{
		Name:      "Ducktopia",
		MCVersion: "1.21.1",
		Loader:    "neoforge",
		Source:    "modlist",
	}, "jei\n")
	if err != nil {
		t.Fatal(err)
	}

	// GET /minecraft/configs should redirect to /minecraft/1/configs
	req := httptest.NewRequest(fiber.MethodGet, "/minecraft/configs", nil)
	resp, err := s.App.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusTemporaryRedirect {
		t.Fatalf("/minecraft/configs status = %d, want 307", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/minecraft/1/configs" {
		t.Errorf("Location = %q, want /minecraft/1/configs", loc)
	}

	// GET /minecraft/mods should redirect to /minecraft/1/mods
	req2 := httptest.NewRequest(fiber.MethodGet, "/minecraft/mods", nil)
	resp2, err := s.App.Test(req2)
	if err != nil {
		t.Fatal(err)
	}
	if resp2.StatusCode != fiber.StatusTemporaryRedirect {
		t.Fatalf("/minecraft/mods status = %d, want 307", resp2.StatusCode)
	}
	if loc := resp2.Header.Get("Location"); loc != "/minecraft/1/mods" {
		t.Errorf("Location = %q, want /minecraft/1/mods", loc)
	}
}
