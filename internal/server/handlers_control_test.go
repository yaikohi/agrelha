package server

import (
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"agrelha/internal/config"
	"agrelha/internal/k8s"
	"agrelha/internal/store"
)

func TestServerControlSSEToast(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cs := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "valheim", Namespace: "valheim"},
	})
	s := &FiberServer{
		App:   fiber.New(),
		cfg:   &config.Config{},
		store: st,
		k8s:   k8s.NewWithClientset(cs, "valheim", "valheim"),
	}
	s.RegisterFiberRoutes()

	resp, err := s.App.Test(httptest.NewRequest(fiber.MethodPost, "/server/restart", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "datastar-patch-signals") {
		t.Fatalf("body missing datastar signal frame: %q", body)
	}
	if !strings.Contains(string(body), "toast") {
		t.Fatalf("body missing toast signal: %q", body)
	}
	if n := countAudit(t, st); n != 1 {
		t.Fatalf("audit rows = %d, want 1", n)
	}
}

func TestServerControlNoK8sNoAudit(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	s := &FiberServer{App: fiber.New(), cfg: &config.Config{}, store: st}
	s.RegisterFiberRoutes()

	resp, err := s.App.Test(httptest.NewRequest(fiber.MethodPost, "/server/stop", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if n := countAudit(t, st); n != 0 {
		t.Fatalf("audit rows = %d, want 0 (no cluster)", n)
	}
}

func countAudit(t *testing.T, st *store.Store) int {
	t.Helper()
	rows, err := st.ListHistory(10)
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

func TestMinecraftServerControlSSEToast(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cs := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "minecraft-neoforge", Namespace: "minecraft-neoforge"},
	})
	s := &FiberServer{
		App:   fiber.New(),
		cfg:   &config.Config{},
		store: st,
		mck8s: k8s.NewWithClientset(cs, "minecraft-neoforge", "minecraft-neoforge"),
	}
	s.RegisterFiberRoutes()

	resp, err := s.App.Test(httptest.NewRequest(fiber.MethodPost, "/minecraft/server/restart", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "toast") {
		t.Fatalf("body missing toast signal: %q", body)
	}
	if n := countAudit(t, st); n != 1 {
		t.Fatalf("audit rows = %d, want 1", n)
	}
}

func TestRootDashboard(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	s := &FiberServer{
		App:   fiber.New(),
		cfg:   &config.Config{GrafanaDashboardURL: "https://grafana.example.com"},
		store: st,
	}
	s.RegisterFiberRoutes()

	resp, err := s.App.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	content := string(body)
	if !strings.Contains(content, "Game Server Hub") {
		t.Errorf("expected 'Game Server Hub' in body")
	}
	if !strings.Contains(content, "Valheim Dedicated") {
		t.Errorf("expected 'Valheim Dedicated' in body")
	}
	if !strings.Contains(content, "Minecraft Worlds") {
		t.Errorf("expected 'Minecraft Worlds' in body")
	}
	if !strings.Contains(content, "Open Minecraft Manager") {
		t.Errorf("expected 'Open Minecraft Manager' in body")
	}
}
