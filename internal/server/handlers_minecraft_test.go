package server

import (
	"io"
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
		cm("minecraft-modded-mods", map[string]string{
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
			wantStatus: fiber.StatusTemporaryRedirect,
			wantBody:   "",
		},
		{
			name:       "minecraft fabric mods page",
			method:     fiber.MethodGet,
			path:       "/minecraft/mods?loader=fabric",
			wantStatus: fiber.StatusTemporaryRedirect,
			wantBody:   "",
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
			wantStatus: fiber.StatusTemporaryRedirect,
			wantBody:   "",
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
		{
			name:       "minecraft mods page modpacks tab",
			method:     fiber.MethodGet,
			path:       "/minecraft/mods?tab=modpacks",
			wantStatus: fiber.StatusTemporaryRedirect,
			wantBody:   "",
		},
		{
			name:       "minecraft mods page individual mods tab",
			method:     fiber.MethodGet,
			path:       "/minecraft/mods?tab=mods",
			wantStatus: fiber.StatusTemporaryRedirect,
			wantBody:   "",
		},
		{
			name:       "minecraft access page whitelist enforcement",
			method:     fiber.MethodGet,
			path:       "/minecraft/access",
			wantStatus: fiber.StatusOK,
			wantBody:   "Whitelist Enforcement",
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

func TestMinecraftWhitelistToggle(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	s := &FiberServer{
		App:   fiber.New(),
		cfg:   &config.Config{},
		store: st,
	}
	s.RegisterFiberRoutes()

	// 1. Without mcAccess configured, API returns 503
	resp, err := s.App.Test(httptest.NewRequest(fiber.MethodPost, "/api/minecraft/access/whitelist/toggle", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}

	// 2. HTML form redirect when unconfigured
	formReq := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/access/whitelist/toggle", nil)
	formReq.Header.Set("Accept", "text/html")
	resp, err = s.App.Test(formReq)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("status = %d, want 303 redirect", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/minecraft/access" {
		t.Fatalf("location = %q, want /minecraft/access", loc)
	}
}
