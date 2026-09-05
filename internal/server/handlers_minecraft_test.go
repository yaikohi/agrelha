package server

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"agrelha/internal/config"
	"agrelha/internal/k8s"
	"agrelha/internal/modrinth"
	"agrelha/internal/store"
)

func TestMinecraftEndpoints(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cm := func(name string, data map[string]string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "minecraft-neoforge"},
			Data:       data,
		}
	}
	cs := fake.NewSimpleClientset(
		cm("minecraft-neoforge-mods", map[string]string{
			"MINECRAFT_VERSION": "1.21.1",
			"NEOFORGE_VERSION":  "recommended",
			"mods.txt":          "jei\nferrite-core\n",
		}),
		cm("minecraft-neoforge-access", map[string]string{
			"ops.txt":       "ykhi\n",
			"whitelist.txt": "ykhi\nfriend\n",
		}),
		cm("minecraft-neoforge-configs", map[string]string{
			"test.toml": "key = \"val\"\n",
		}),
	)

	s := &FiberServer{
		App:   fiber.New(),
		cfg:   &config.Config{},
		store: st,
		mck8s: k8s.NewWithClientset(cs, "minecraft-neoforge", "minecraft-neoforge"),
	}
	s.RegisterFiberRoutes()

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "minecraft mods page",
			method:     fiber.MethodGet,
			path:       "/minecraft/mods",
			wantStatus: fiber.StatusOK,
			wantBody:   "Minecraft NeoForge",
		},
		{
			name:       "minecraft access page",
			method:     fiber.MethodGet,
			path:       "/minecraft/access",
			wantStatus: fiber.StatusOK,
			wantBody:   "Minecraft Access Management",
		},
		{
			name:       "minecraft configs page",
			method:     fiber.MethodGet,
			path:       "/minecraft/configs",
			wantStatus: fiber.StatusOK,
			wantBody:   "Minecraft Mod Configs",
		},
		{
			name:       "minecraft config new page",
			method:     fiber.MethodGet,
			path:       "/minecraft/configs/new",
			wantStatus: fiber.StatusOK,
			wantBody:   "New Minecraft Config File",
		},
		{
			name:       "minecraft config edit page",
			method:     fiber.MethodGet,
			path:       "/minecraft/configs/edit?f=test.toml",
			wantStatus: fiber.StatusOK,
			wantBody:   "test.toml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			resp, err := s.App.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), tt.wantBody) {
				t.Fatalf("body did not contain %q", tt.wantBody)
			}
		})
	}
}

func TestMinecraftModpackExportEndpoint(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Mock Modrinth API server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/project/jei/version") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{
					"version_number": "19.21.0.246",
					"files": [
						{
							"filename": "jei-1.21.1-neoforge-19.21.0.246.jar",
							"url": "https://cdn.example.com/jei.jar",
							"primary": true,
							"size": 123456,
							"hashes": {
								"sha1": "sha1jei",
								"sha512": "sha512jei"
							}
						}
					]
				}
			]`))
			return
		}
		if strings.HasPrefix(path, "/project/jei") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"slug": "jei",
				"client_side": "optional",
				"server_side": "optional"
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cm := func(name string, data map[string]string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "minecraft-neoforge"},
			Data:       data,
		}
	}
	cs := fake.NewSimpleClientset(
		cm("minecraft-neoforge-mods", map[string]string{
			"MINECRAFT_VERSION": "1.21.1",
			"NEOFORGE_VERSION":  "21.1.249",
			"mods.txt":          "jei\n",
		}),
		cm("minecraft-neoforge-configs", map[string]string{
			"jei-client.ini": "showCheats = true\n",
		}),
	)

	s := &FiberServer{
		App:   fiber.New(),
		cfg:   &config.Config{},
		store: st,
		mck8s: k8s.NewWithClientset(cs, "minecraft-neoforge", "minecraft-neoforge"),
		mr:    modrinth.New(ts.URL),
	}
	s.RegisterFiberRoutes()

	resp, err := s.App.Test(httptest.NewRequest(fiber.MethodGet, "/minecraft/mods/export", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-modrinth-modpack+zip" {
		t.Errorf("content-type = %q, want application/x-modrinth-modpack+zip", ct)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.Contains(cd, ".mrpack") {
		t.Errorf("content-disposition = %q, want .mrpack", cd)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("not a valid zip: %v", err)
	}

	var foundIndex, foundConfig bool
	for _, f := range zr.File {
		if f.Name == "modrinth.index.json" {
			foundIndex = true
		}
		if f.Name == "overrides/config/jei-client.ini" {
			foundConfig = true
		}
	}

	if !foundIndex {
		t.Errorf("modrinth.index.json missing from mrpack")
	}
	if !foundConfig {
		t.Errorf("overrides/config/jei-client.ini missing from mrpack")
	}
}
