package server

import (
	"encoding/json"
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

func TestServerControlReturnsSignalsPatch(t *testing.T) {
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
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type = %q, want application/json (Datastar signals patch)", ct)
	}
	blob, _ := io.ReadAll(resp.Body)
	var got map[string]any
	if err := json.Unmarshal(blob, &got); err != nil {
		t.Fatalf("body not JSON: %v (%s)", err, blob)
	}
	msg, ok := got["toast"].(string)
	if !ok || msg == "" {
		t.Fatalf("missing toast signal in %s", blob)
	}
}

func TestServerControlNoK8sIsGraceful(t *testing.T) {
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
	blob, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(blob), "toast") {
		t.Fatalf("expected a toast signal, got %s", blob)
	}
}
