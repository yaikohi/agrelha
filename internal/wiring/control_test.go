package wiring_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	auth "agrelha/internal/infra/auth/oidc"
	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/store"
	"agrelha/internal/platform/config"
	"agrelha/internal/wiring"
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
	app := wiring.BuildServer(context.Background(), &config.Config{}, wiring.Deps{
		Store: st,
		K8s:   k8s.NewWithClientset(cs, "valheim", "valheim"),
	})

	resp, err := app.Test(httptest.NewRequest(fiber.MethodPost, "/server/restart", nil))
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

	app := wiring.BuildServer(context.Background(), &config.Config{}, wiring.Deps{Store: st})

	resp, err := app.Test(httptest.NewRequest(fiber.MethodPost, "/server/stop", nil))
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
	cfg := &config.Config{
		MinecraftDeployment: "minecraft-neoforge",
		MinecraftNamespace:  "minecraft-neoforge",
	}
	app := wiring.BuildServer(context.Background(), cfg, wiring.Deps{
		Store: st,
		MCK8s: k8s.NewWithClientset(cs, "minecraft-neoforge", "minecraft-neoforge"),
	})

	resp, err := app.Test(httptest.NewRequest(fiber.MethodPost, "/minecraft/server/restart", nil))
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

	app := wiring.BuildServer(context.Background(), &config.Config{GrafanaDashboardURL: "https://grafana.example.com"}, wiring.Deps{
		Store: st,
	})

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
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

func TestGuestRootDashboardAndAuthProtection(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	app := wiring.BuildServer(context.Background(), &config.Config{}, wiring.Deps{
		Store: st,
		Auth:  &auth.Authenticator{},
	})

	// 1. Guest visits / -> 200 OK (Public player portal view)
	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("GET / status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	content := string(body)
	if !strings.Contains(content, "Community Game Servers") {
		t.Errorf("expected 'Community Game Servers' in guest body")
	}
	if !strings.Contains(content, "Admin Sign In") {
		t.Errorf("expected 'Admin Sign In' button in guest nav")
	}
	if !strings.Contains(content, "Download Modpack") {
		t.Errorf("expected 'Download Modpack' in guest view")
	}
	if strings.Contains(content, "Open Valheim Manager") {
		t.Errorf("guest view should NOT have 'Open Valheim Manager'")
	}
	if strings.Contains(content, "Open Minecraft Manager") {
		t.Errorf("guest view should NOT have 'Open Minecraft Manager'")
	}
	if strings.Contains(content, "Sign out") {
		t.Errorf("guest view should NOT have 'Sign out'")
	}

	// 2. Protected admin routes require login -> redirect to /auth/login
	for _, path := range []string{"/minecraft", "/mods", "/configs", "/admins", "/history"} {
		req := httptest.NewRequest(fiber.MethodGet, path, nil)
		r, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if r.StatusCode != fiber.StatusFound {
			t.Fatalf("GET %s status = %d, want 302", path, r.StatusCode)
		}
		loc := r.Header.Get("Location")
		if !strings.HasPrefix(loc, "/auth/login") {
			t.Fatalf("GET %s redirect location = %q, want prefix /auth/login", path, loc)
		}
	}

	// 3. /login redirects directly to /auth/login
	loginReq := httptest.NewRequest(fiber.MethodGet, "/login", nil)
	loginResp, err := app.Test(loginReq)
	if err != nil {
		t.Fatal(err)
	}
	if loginResp.StatusCode != fiber.StatusFound {
		t.Fatalf("GET /login status = %d, want 302", loginResp.StatusCode)
	}
	if loc := loginResp.Header.Get("Location"); loc != "/auth/login" {
		t.Fatalf("GET /login location = %q, want /auth/login", loc)
	}

	// 4. /auth/logout clears session cookies and redirects to /
	logoutReq := httptest.NewRequest(fiber.MethodGet, "/auth/logout", nil)
	logoutResp, err := app.Test(logoutReq)
	if err != nil {
		t.Fatal(err)
	}
	if logoutResp.StatusCode != fiber.StatusFound {
		t.Fatalf("GET /auth/logout status = %d, want 302", logoutResp.StatusCode)
	}
	if loc := logoutResp.Header.Get("Location"); loc != "/" {
		t.Fatalf("GET /auth/logout location = %q, want /", loc)
	}
	cookies := logoutResp.Header.Values("Set-Cookie")
	hasClearedSession := false
	for _, c := range cookies {
		low := strings.ToLower(c)
		if strings.Contains(c, "agrelha_session=") && strings.Contains(low, "path=/") && strings.Contains(low, "secure") {
			hasClearedSession = true
			break
		}
	}
	if !hasClearedSession {
		t.Fatalf("expected Set-Cookie clearing agrelha_session with path=/ and secure, got: %v", cookies)
	}
}

func TestDevAuthenticatorLifecycle(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cfg := &config.Config{
		AllowedEmail: "ykhi@proton.me",
	}
	devAuth := auth.NewDev(auth.Config{
		AllowedEmail: cfg.AllowedEmail,
	})
	app := wiring.BuildServer(context.Background(), cfg, wiring.Deps{
		Store: st,
		Auth:  devAuth,
	})

	// 1. Initially guest
	req := httptest.NewRequest(fiber.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Admin Sign In") {
		t.Fatalf("expected Admin Sign In for unauthenticated dev visitor")
	}

	// 2. Protected admin route redirects to /auth/login
	protReq := httptest.NewRequest(fiber.MethodGet, "/mods", nil)
	protResp, err := app.Test(protReq)
	if err != nil {
		t.Fatal(err)
	}
	if protResp.StatusCode != fiber.StatusFound || !strings.HasPrefix(protResp.Header.Get("Location"), "/auth/login") {
		t.Fatalf("expected 302 with prefix /auth/login, got status %d loc %s", protResp.StatusCode, protResp.Header.Get("Location"))
	}

	// 3. Sign in via dev login
	loginReq := httptest.NewRequest(fiber.MethodGet, "/auth/login", nil)
	loginResp, err := app.Test(loginReq)
	if err != nil {
		t.Fatal(err)
	}
	if loginResp.StatusCode != fiber.StatusFound || loginResp.Header.Get("Location") != "/" {
		t.Fatalf("expected dev login to redirect to /, got status %d loc %s", loginResp.StatusCode, loginResp.Header.Get("Location"))
	}
	var sessionCookieVal string
	for _, c := range loginResp.Header.Values("Set-Cookie") {
		if strings.HasPrefix(c, "agrelha_session=") {
			parts := strings.Split(c, ";")
			sessionCookieVal = strings.TrimPrefix(parts[0], "agrelha_session=")
			break
		}
	}
	if sessionCookieVal == "" {
		t.Fatalf("dev login did not set agrelha_session cookie")
	}

	// 4. Visit / with session cookie -> Admin dashboard
	adminReq := httptest.NewRequest(fiber.MethodGet, "/", nil)
	adminReq.AddCookie(&http.Cookie{Name: "agrelha_session", Value: sessionCookieVal})
	adminResp, err := app.Test(adminReq)
	if err != nil {
		t.Fatal(err)
	}
	adminBody, _ := io.ReadAll(adminResp.Body)
	if !strings.Contains(string(adminBody), "Sign out") {
		t.Fatalf("expected Sign out in admin dashboard")
	}

	// 5. Sign out
	logoutReq := httptest.NewRequest(fiber.MethodGet, "/auth/logout", nil)
	logoutReq.AddCookie(&http.Cookie{Name: "agrelha_session", Value: sessionCookieVal})
	logoutResp, err := app.Test(logoutReq)
	if err != nil {
		t.Fatal(err)
	}
	if logoutResp.StatusCode != fiber.StatusFound || logoutResp.Header.Get("Location") != "/" {
		t.Fatalf("expected logout to redirect to /, got status %d loc %s", logoutResp.StatusCode, logoutResp.Header.Get("Location"))
	}
}
