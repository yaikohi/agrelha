package console

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/store"

	"github.com/gofiber/fiber/v2"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestValheimConsolePage(t *testing.T) {
	h := New(Config{})
	app := fiber.New()
	app.Get("/valheim", h.ValheimConsole)

	req := httptest.NewRequest(http.MethodGet, "/valheim", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestServerLifecycleWithoutK8s(t *testing.T) {
	h := New(Config{})
	app := fiber.New()
	h.Register(app)

	endpoints := []string{
		"/server/restart",
		"/server/update",
		"/server/stop",
		"/server/start",
	}

	for _, ep := range endpoints {
		req := httptest.NewRequest(http.MethodPost, ep, nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s: %v", ep, err)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "Imperative plane disabled") {
			t.Errorf("%s response = %s, want Imperative plane disabled", ep, string(body))
		}
	}
}

func TestServerLifecycleWithK8s(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "console_test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cs := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "valheim", Namespace: "valheim"},
	})
	k8sClient := k8s.NewWithClientset(cs, "valheim", "valheim")

	h := New(Config{
		K8s:   k8sClient,
		Store: st,
	})
	app := fiber.New()
	h.Register(app)

	// 1. Restart
	reqRestart := httptest.NewRequest(http.MethodPost, "/server/restart", nil)
	respRestart, err := app.Test(reqRestart)
	if err != nil {
		t.Fatal(err)
	}
	bodyRestart, _ := io.ReadAll(respRestart.Body)
	if !strings.Contains(string(bodyRestart), "Restart triggered") {
		t.Errorf("restart response = %s", string(bodyRestart))
	}

	// 2. Stop (scale to 0)
	reqStop := httptest.NewRequest(http.MethodPost, "/server/stop", nil)
	respStop, err := app.Test(reqStop)
	if err != nil {
		t.Fatal(err)
	}
	bodyStop, _ := io.ReadAll(respStop.Body)
	if !strings.Contains(string(bodyStop), "Stopping the server") {
		t.Errorf("stop response = %s", string(bodyStop))
	}

	// 3. Start (scale to 1)
	reqStart := httptest.NewRequest(http.MethodPost, "/server/start", nil)
	respStart, err := app.Test(reqStart)
	if err != nil {
		t.Fatal(err)
	}
	bodyStart, _ := io.ReadAll(respStart.Body)
	if !strings.Contains(string(bodyStart), "Starting the server") {
		t.Errorf("start response = %s", string(bodyStart))
	}
}

func TestMCRconAndLogsUnconfigured(t *testing.T) {
	h := New(Config{})
	app := fiber.New()
	h.Register(app)

	// 1. RCON unconfigured
	reqRcon := httptest.NewRequest(http.MethodPost, "/api/minecraft/1/rcon", strings.NewReader("cmd=help"))
	reqRcon.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respRcon, err := app.Test(reqRcon)
	if err != nil {
		t.Fatal(err)
	}
	bodyRcon, _ := io.ReadAll(respRcon.Body)
	if !strings.Contains(string(bodyRcon), "unconfigured") {
		t.Errorf("rcon response = %s", string(bodyRcon))
	}

	// 2. Logs unconfigured
	reqLogs := httptest.NewRequest(http.MethodGet, "/api/minecraft/1/logs", nil)
	respLogs, err := app.Test(reqLogs)
	if err != nil {
		t.Fatal(err)
	}
	bodyLogs, _ := io.ReadAll(respLogs.Body)
	if !strings.Contains(string(bodyLogs), "Logs unavailable") {
		t.Errorf("logs response = %s", string(bodyLogs))
	}
}
