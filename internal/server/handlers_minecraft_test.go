package server

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	appsv1 "k8s.io/api/apps/v1"
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
			wantStatus: fiber.StatusOK,
			wantBody:   "Minecraft neoforge",
		},
		{
			name:       "minecraft fabric mods page",
			method:     fiber.MethodGet,
			path:       "/minecraft/mods?loader=fabric",
			wantStatus: fiber.StatusOK,
			wantBody:   "Minecraft fabric",
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
		{
			name:       "minecraft mods page modpacks tab",
			method:     fiber.MethodGet,
			path:       "/minecraft/mods?tab=modpacks",
			wantStatus: fiber.StatusOK,
			wantBody:   "Modpack Switcher",
		},
		{
			name:       "minecraft mods page individual mods tab",
			method:     fiber.MethodGet,
			path:       "/minecraft/mods?tab=mods",
			wantStatus: fiber.StatusOK,
			wantBody:   "Modrinth Mod Discovery",
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
		cm("minecraft-modded-mods", map[string]string{
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

func TestMinecraftLoaderSwitch(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	r1 := int32(1)
	cs := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "minecraft-modded", Namespace: "minecraft-modded"},
			Spec:       appsv1.DeploymentSpec{Replicas: &r1},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "minecraft-modded-slot", Namespace: "minecraft-modded"},
			Data: map[string]string{
				"WORLD_SLOT": "ducktopia",
				"TYPE":       "AUTO_CURSEFORGE",
			},
		},
	)

	s := &FiberServer{
		App: fiber.New(),
		cfg: &config.Config{
			MinecraftNamespace:  "minecraft-modded",
			MinecraftDeployment: "minecraft-modded",
		},
		store: st,
		mck8s: k8s.NewWithClientset(cs, "minecraft-modded", "minecraft-modded"),
	}
	s.RegisterFiberRoutes()

	post := func(target, accept string) int {
		req := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/loader/switch", strings.NewReader("target="+target))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		resp, err := s.App.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode
	}

	if code := post("forge", ""); code != fiber.StatusBadRequest {
		t.Fatalf("invalid target: status = %d, want 400", code)
	}

	// Switching now rewrites the slot ConfigMap through the GitOps plane rather
	// than scaling two deployments, so with no committer configured it must
	// refuse rather than silently no-op.
	if code := post("fabric", ""); code != fiber.StatusServiceUnavailable {
		t.Fatalf("no gitops: status = %d, want 503", code)
	}

	slot, typ, err := s.mck8s.ActiveSlot(context.Background(), "minecraft-modded-slot")
	if err != nil {
		t.Fatal(err)
	}
	if slot != "ducktopia" || typ != "AUTO_CURSEFORGE" {
		t.Fatalf("ActiveSlot() = %q/%q, want ducktopia/AUTO_CURSEFORGE", slot, typ)
	}
}

func TestMinecraftFabricExportEndpoint(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Mock Modrinth API server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/project/fabric-api/version") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{
					"version_number": "0.100.0",
					"files": [
						{
							"filename": "fabric-api-0.100.0.jar",
							"primary": true,
							"size": 2048,
							"url": "https://cdn.modrinth.com/data/fabric-api.jar",
							"hashes": {"sha1": "abc", "sha512": "def"}
						}
					]
				}
			]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cm := func(name string, data map[string]string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "minecraft-modded"},
			Data:       data,
		}
	}
	cs := fake.NewSimpleClientset(
		cm("minecraft-modded-mods", map[string]string{
			"MINECRAFT_VERSION": "1.21.1",
			"FABRIC_VERSION":    "0.16.5",
			"mods.txt":          "fabric-api\n",
		}),
		cm("minecraft-fabric-configs", map[string]string{
			"fabric.json": "{\"test\": true}\n",
		}),
	)

	s := &FiberServer{
		App: fiber.New(),
		cfg: &config.Config{
			MinecraftNamespace:  "minecraft-modded",
			MinecraftDeployment: "minecraft-neoforge",
			FabricDeployment:    "minecraft-fabric",
		},
		store: st,
		mck8s: k8s.NewWithClientset(cs, "minecraft-modded", "minecraft-neoforge"),
		mr:    modrinth.New(ts.URL),
	}
	s.RegisterFiberRoutes()

	req := httptest.NewRequest(fiber.MethodGet, "/minecraft/mods/export?loader=fabric", nil)
	resp, err := s.App.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-modrinth-modpack+zip" {
		t.Fatalf("content-type = %s, want application/x-modrinth-modpack+zip", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}

	var foundIndex, foundConfig bool
	for _, f := range zr.File {
		if f.Name == "modrinth.index.json" {
			foundIndex = true
		}
		if f.Name == "overrides/config/fabric.json" {
			foundConfig = true
		}
	}

	if !foundIndex {
		t.Errorf("modrinth.index.json missing from fabric mrpack")
	}
	if !foundConfig {
		t.Errorf("overrides/config/fabric.json missing from fabric mrpack")
	}
}

func TestWorldsTabListsSlots(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	for _, sl := range []store.Slot{
		{Slot: "ducktopia", Type: "AUTO_CURSEFORGE", Pack: "Ducktopia Farlands"},
		{Slot: "atm9", Type: "NEOFORGE", Pack: "All The Mods 9", MCVersion: "1.21.1"},
	} {
		if err := st.UpsertSlot(sl); err != nil {
			t.Fatal(err)
		}
	}

	cs := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "minecraft-modded-slot", Namespace: "minecraft-modded"},
		Data:       map[string]string{"WORLD_SLOT": "ducktopia", "TYPE": "AUTO_CURSEFORGE"},
	})

	s := &FiberServer{
		App:   fiber.New(),
		cfg:   &config.Config{MinecraftNamespace: "minecraft-modded", MinecraftDeployment: "minecraft-modded"},
		store: st,
		mck8s: k8s.NewWithClientset(cs, "minecraft-modded", "minecraft-modded"),
	}
	s.RegisterFiberRoutes()

	resp, err := s.App.Test(httptest.NewRequest(fiber.MethodGet, "/minecraft/mods?tab=worlds", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	for _, want := range []string{"World slots", "ducktopia", "atm9", "All The Mods 9", "RUNNING"} {
		if !strings.Contains(html, want) {
			t.Fatalf("worlds tab missing %q", want)
		}
	}
	// The running slot must not offer a switch button for itself.
	if strings.Count(html, "Switch to this world") != 1 {
		t.Fatalf("want exactly one switch button (for the inactive slot), got %d", strings.Count(html, "Switch to this world"))
	}
}

func TestSlotSwitchRejectsUnknownSlot(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	s := &FiberServer{App: fiber.New(), cfg: &config.Config{}, store: st}
	s.RegisterFiberRoutes()

	req := httptest.NewRequest(fiber.MethodPost, "/api/minecraft/slot/switch", strings.NewReader("slot="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.App.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("empty slot: status = %d, want 400", resp.StatusCode)
	}
}
