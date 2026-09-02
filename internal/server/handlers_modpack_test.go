package server

import (
	"archive/zip"
	"bytes"
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

func TestModpackExportEndpoint(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cm := func(name string, data map[string]string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "valheim"},
			Data:       data,
		}
	}
	cs := fake.NewSimpleClientset(
		cm("valheim-mods", map[string]string{"mods.txt": "# my server\ndenikson/BepInExPack_Valheim/5.4.2202\nValheimModding/Jotunn/2.24.3\n"}),
		cm("valheim-mod-configs", map[string]string{"com.jotunn.jotunn.cfg": "[General]\nEnabled = true\n"}),
	)

	s := &FiberServer{
		App:   fiber.New(),
		cfg:   &config.Config{},
		store: st,
		k8s:   k8s.NewWithClientset(cs, "valheim", "valheim"),
	}
	s.RegisterFiberRoutes()

	resp, err := s.App.Test(httptest.NewRequest(fiber.MethodGet, "/mods/export", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("content-type = %q", ct)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.Contains(cd, ".r2z") {
		t.Fatalf("content-disposition = %q", cd)
	}

	blob, _ := io.ReadAll(resp.Body)
	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = string(b)
	}
	r2x, ok := files["export.r2x"]
	if !ok {
		t.Fatalf("no export.r2x; files=%v", files)
	}
	for _, want := range []string{
		"profileName: Valheim (ykhi)",
		"name: denikson-BepInExPack_Valheim",
		"name: ValheimModding-Jotunn",
		"major: 5", "minor: 4", "patch: 2202",
		"enabled: true",
	} {
		if !strings.Contains(r2x, want) {
			t.Fatalf("export.r2x missing %q:\n%s", want, r2x)
		}
	}
	if _, ok := files["config/com.jotunn.jotunn.cfg"]; !ok {
		t.Fatalf("config not under config/; files=%v", files)
	}
	t.Logf("export.r2x:\n%s", r2x)
}

func TestModpackExportInjectsBepInEx(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cm := func(name string, data map[string]string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "valheim"},
			Data:       data,
		}
	}
	// mods.txt WITHOUT a BepInExPack entry (mirrors the real server, which
	// installs BepInEx itself) — the export must still inject it for r2modman.
	cs := fake.NewSimpleClientset(
		cm("valheim-mods", map[string]string{"mods.txt": "ValheimModding/Jotunn/2.24.3\n"}),
	)
	s := &FiberServer{
		App:   fiber.New(),
		cfg:   &config.Config{},
		store: st,
		k8s:   k8s.NewWithClientset(cs, "valheim", "valheim"),
	}
	s.RegisterFiberRoutes()

	resp, err := s.App.Test(httptest.NewRequest(fiber.MethodGet, "/mods/export", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	blob, _ := io.ReadAll(resp.Body)
	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	var r2x string
	for _, f := range zr.File {
		if f.Name == "export.r2x" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			r2x = string(b)
		}
	}
	if !strings.Contains(r2x, "name: denikson-BepInExPack_Valheim") {
		t.Fatalf("export must inject BepInExPack when absent from mods.txt:\n%s", r2x)
	}
}
