package local

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"agrelha/internal/ports"

	"github.com/gofiber/fiber/v2"
)

type mockUserStore struct {
	users  map[string]ports.User
	hashes map[string]string
}

func newMockUserStore() *mockUserStore {
	return &mockUserStore{
		users:  make(map[string]ports.User),
		hashes: make(map[string]string),
	}
}

func (m *mockUserStore) GetUser(_ context.Context, username string) (*ports.User, string, error) {
	u, ok := m.users[username]
	if !ok {
		return nil, "", nil
	}
	return &u, m.hashes[username], nil
}

func (m *mockUserStore) CreateUser(_ context.Context, user ports.User, passwordHash string) error {
	m.users[user.Username] = user
	m.hashes[user.Username] = passwordHash
	return nil
}

func (m *mockUserStore) ListUsers(_ context.Context) ([]ports.User, error) {
	out := make([]ports.User, 0, len(m.users))
	for _, u := range m.users {
		out = append(out, u)
	}
	return out, nil
}

func (m *mockUserStore) DeleteUser(_ context.Context, username string) error {
	delete(m.users, username)
	delete(m.hashes, username)
	return nil
}

var _ ports.UserStore = (*mockUserStore)(nil)

func TestArgon2idHashAndVerify(t *testing.T) {
	password := "super-secret-password-123"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Fatalf("unexpected hash format: %s", hash)
	}

	// Verify correct password
	match, err := VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("VerifyPassword error: %v", err)
	}
	if !match {
		t.Fatalf("expected password match, got false")
	}

	// Verify incorrect password
	matchWrong, err := VerifyPassword("wrong-password", hash)
	if err != nil {
		t.Fatalf("VerifyPassword wrong error: %v", err)
	}
	if matchWrong {
		t.Fatalf("expected password mismatch for wrong password, got true")
	}

	// Verify invalid hash
	_, errInvalid := VerifyPassword(password, "invalid-hash-string")
	if errInvalid == nil {
		t.Fatalf("expected error for invalid hash format, got nil")
	}
}

func TestLocalAuthenticatorFlow(t *testing.T) {
	store := newMockUserStore()
	hash, err := HashPassword("adminpass")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	_ = store.CreateUser(context.Background(), ports.User{
		Username:  "admin",
		Email:     "admin@example.com",
		CreatedAt: time.Now(),
	}, hash)

	auth := New(store, "test-secret-key", WithSecureCookie(false))
	var _ ports.Auth = auth

	app := fiber.New()
	app.Get("/auth/login", auth.Login)
	app.Post("/auth/login", auth.Login)
	app.Get("/auth/logout", auth.Logout)

	protected := app.Group("/admin", auth.Middleware())
	protected.Get("/dashboard", func(c *fiber.Ctx) error {
		return c.SendString("Welcome, " + c.Locals("actor").(string))
	})

	// 1. Unauthenticated request to protected route redirects to /auth/login
	req1 := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	resp1, err := app.Test(req1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp1.StatusCode != http.StatusFound {
		t.Errorf("unauthenticated status = %d, want 302", resp1.StatusCode)
	}
	if loc := resp1.Header.Get("Location"); !strings.HasPrefix(loc, "/auth/login") {
		t.Errorf("Location = %q, want prefix /auth/login", loc)
	}

	// 2. Unauthenticated Datastar SSE request receives SSE redirect script
	reqSSE := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	reqSSE.Header.Set("Datastar-Request", "true")
	respSSE, err := app.Test(reqSSE)
	if err != nil {
		t.Fatalf("app.Test SSE failed: %v", err)
	}
	bodySSE, _ := io.ReadAll(respSSE.Body)
	if !strings.Contains(string(bodySSE), "datastar-patch-elements") || !strings.Contains(string(bodySSE), "window.location.href") {
		t.Errorf("expected SSE redirect script, got: %s", string(bodySSE))
	}

	// 3. GET /auth/login returns HTML form
	reqGetLogin := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	respGetLogin, err := app.Test(reqGetLogin)
	if err != nil {
		t.Fatalf("GET /auth/login failed: %v", err)
	}
	if respGetLogin.StatusCode != http.StatusOK {
		t.Errorf("GET /auth/login status = %d, want 200", respGetLogin.StatusCode)
	}

	// 4. POST /auth/login with invalid password fails
	badForm := url.Values{"username": {"admin"}, "password": {"badpass"}}.Encode()
	reqBadLogin := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(badForm))
	reqBadLogin.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respBadLogin, err := app.Test(reqBadLogin)
	if err != nil {
		t.Fatalf("POST bad login failed: %v", err)
	}
	if respBadLogin.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad login status = %d, want 401", respBadLogin.StatusCode)
	}

	// 5. POST /auth/login with valid password succeeds and sets session cookie
	goodForm := url.Values{"username": {"admin"}, "password": {"adminpass"}}.Encode()
	reqGoodLogin := httptest.NewRequest(http.MethodPost, "/auth/login?returnTo=/admin/dashboard", strings.NewReader(goodForm))
	reqGoodLogin.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respGoodLogin, err := app.Test(reqGoodLogin)
	if err != nil {
		t.Fatalf("POST good login failed: %v", err)
	}
	if respGoodLogin.StatusCode != http.StatusFound {
		t.Errorf("good login status = %d, want 302", respGoodLogin.StatusCode)
	}
	cookie := respGoodLogin.Header.Get("Set-Cookie")
	if !strings.Contains(cookie, sessionCookie) {
		t.Fatalf("expected Set-Cookie with %s, got: %s", sessionCookie, cookie)
	}

	// Extract cookie value
	var cookieVal string
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, sessionCookie+"=") {
			cookieVal = strings.TrimPrefix(part, sessionCookie+"=")
			break
		}
	}

	// 6. Access protected route with valid session cookie
	reqAuthed := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	reqAuthed.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookieVal})
	respAuthed, err := app.Test(reqAuthed)
	if err != nil {
		t.Fatalf("authed request failed: %v", err)
	}
	if respAuthed.StatusCode != http.StatusOK {
		t.Errorf("authed request status = %d, want 200", respAuthed.StatusCode)
	}
	authedBody, _ := io.ReadAll(respAuthed.Body)
	if string(authedBody) != "Welcome, admin" {
		t.Errorf("body = %q, want 'Welcome, admin'", string(authedBody))
	}

	// 7. GET /auth/logout clears session cookie
	reqLogout := httptest.NewRequest(http.MethodGet, "/auth/logout", nil)
	reqLogout.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookieVal})
	respLogout, err := app.Test(reqLogout)
	if err != nil {
		t.Fatalf("logout request failed: %v", err)
	}
	if respLogout.StatusCode != http.StatusFound {
		t.Errorf("logout status = %d, want 302", respLogout.StatusCode)
	}
	logoutCookie := respLogout.Header.Get("Set-Cookie")
	if !strings.Contains(logoutCookie, sessionCookie+"=;") && !strings.Contains(logoutCookie, "Max-Age=-1") {
		t.Errorf("expected cleared cookie in logout, got: %s", logoutCookie)
	}
}
