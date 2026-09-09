package wiring_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/infra/auth/local"
	"agrelha/internal/infra/store"
	"agrelha/internal/platform/config"
	"agrelha/internal/ports"
	"agrelha/internal/server"
	"agrelha/internal/wiring"
)

func build(t *testing.T, cfg *config.Config) *server.FiberServer {
	t.Helper()
	deps, err := wiring.Build(context.Background(), cfg)
	if err != nil {
		t.Fatalf("wiring.Build: %v", err)
	}
	t.Cleanup(func() { _ = deps.Store.Close() })
	return server.New(cfg, deps)
}

func TestLocalAuthFlow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "local_auth.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Seed a local user account
	hash, err := local.HashPassword("hunter2secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUser(t.Context(), ports.User{Username: "localadmin", Email: "admin@example.com"}, hash); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		DBPath:           dbPath,
		OIDCClientSecret: "test-secret-key-12345",
	}
	// New detects local users in store and configures local authenticator
	s := build(t, cfg)
	s.RegisterFiberRoutes()

	// 1. Unauthenticated request to protected route redirects to /auth/login
	protReq := httptest.NewRequest(fiber.MethodGet, "/minecraft", nil)
	protResp, err := s.App.Test(protReq)
	if err != nil {
		t.Fatal(err)
	}
	if protResp.StatusCode != fiber.StatusFound || !strings.HasPrefix(protResp.Header.Get("Location"), "/auth/login") {
		t.Fatalf("expected 302 to /auth/login, got %d loc %s", protResp.StatusCode, protResp.Header.Get("Location"))
	}

	// 2. GET /auth/login serves HTML form
	formReq := httptest.NewRequest(fiber.MethodGet, "/auth/login", nil)
	formResp, err := s.App.Test(formReq)
	if err != nil {
		t.Fatal(err)
	}
	if formResp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 from GET /auth/login, got %d", formResp.StatusCode)
	}

	// 3. POST /auth/login with invalid password fails
	badBody := strings.NewReader("username=localadmin&password=wrongpassword")
	badReq := httptest.NewRequest(fiber.MethodPost, "/auth/login", badBody)
	badReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	badResp, err := s.App.Test(badReq)
	if err != nil {
		t.Fatal(err)
	}
	if badResp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for bad password, got %d", badResp.StatusCode)
	}

	// 4. POST /auth/login with valid credentials succeeds and redirects
	goodBody := strings.NewReader("username=localadmin&password=hunter2secret")
	goodReq := httptest.NewRequest(fiber.MethodPost, "/auth/login?returnTo=/minecraft", goodBody)
	goodReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	goodResp, err := s.App.Test(goodReq)
	if err != nil {
		t.Fatal(err)
	}
	if goodResp.StatusCode != fiber.StatusFound || goodResp.Header.Get("Location") != "/minecraft" {
		t.Fatalf("expected 302 redirect to /minecraft, got %d loc %s", goodResp.StatusCode, goodResp.Header.Get("Location"))
	}

	var sessionCookieVal string
	for _, c := range goodResp.Header.Values("Set-Cookie") {
		if strings.HasPrefix(c, "agrelha_session=") {
			parts := strings.Split(c, ";")
			sessionCookieVal = strings.TrimPrefix(parts[0], "agrelha_session=")
			break
		}
	}
	if sessionCookieVal == "" {
		t.Fatal("expected agrelha_session cookie")
	}

	// 5. Protected route is accessible with session cookie
	authedReq := httptest.NewRequest(fiber.MethodGet, "/minecraft", nil)
	authedReq.AddCookie(&http.Cookie{Name: "agrelha_session", Value: sessionCookieVal})
	authedResp, err := s.App.Test(authedReq)
	if err != nil {
		t.Fatal(err)
	}
	if authedResp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 OK from protected route with valid session, got %d", authedResp.StatusCode)
	}

	// 6. Logout clears session
	logoutReq := httptest.NewRequest(fiber.MethodGet, "/auth/logout", nil)
	logoutReq.AddCookie(&http.Cookie{Name: "agrelha_session", Value: sessionCookieVal})
	logoutResp, err := s.App.Test(logoutReq)
	if err != nil {
		t.Fatal(err)
	}
	if logoutResp.StatusCode != fiber.StatusFound {
		t.Fatalf("expected 302 from logout, got %d", logoutResp.StatusCode)
	}
}

func TestServer_DockerAdapterBootstrap(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		DBPath:        filepath.Join(tempDir, "docker_test.db"),
		Runtime:       "docker",
		LocalStateDir: filepath.Join(tempDir, "state"),
		ComposeDir:    filepath.Join(tempDir, "compose"),
	}

	deps, err := wiring.Build(context.Background(), cfg)
	if err != nil {
		t.Fatalf("wiring.Build: %v", err)
	}
	defer deps.Store.Close()

	if deps.StateStore == nil {
		t.Fatal("expected StateStore to be initialized with the local state store")
	}
	if deps.Reconciler == nil {
		t.Fatal("expected Reconciler to be initialized")
	}
	if deps.Reconciler.Async() {
		t.Errorf("expected compose reconciler to be synchronous (Async() == false)")
	}

	s := server.New(cfg, deps)
	s.RegisterFiberRoutes()

	// Verify routes work
	req := httptest.NewRequest(fiber.MethodGet, "/", nil)
	resp, err := s.App.Test(req)
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("GET / status = %d, want 200", resp.StatusCode)
	}
}
