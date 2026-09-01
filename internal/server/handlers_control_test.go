package server

import (
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

func TestServerControlRedirectsWithFlash(t *testing.T) {
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
	if resp.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Fatalf("location = %q, want /", loc)
	}
	if sc := resp.Header.Get("Set-Cookie"); !strings.Contains(sc, flashCookie) {
		t.Fatalf("no flash cookie set: %q", sc)
	}
	if n := countAudit(t, st); n != 1 {
		t.Fatalf("audit rows = %d, want 1", n)
	}
}

func TestServerControlNoK8sRedirectsWithoutAudit(t *testing.T) {
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
	if resp.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
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
