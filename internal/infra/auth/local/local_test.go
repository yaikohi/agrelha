package local

import (
	"context"
	"encoding/json"
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
	for part := range strings.SplitSeq(cookie, ";") {
		part = strings.TrimSpace(part)
		if after, ok := strings.CutPrefix(part, sessionCookie+"="); ok {
			cookieVal = after
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

func TestLocalAuthenticator_EdgeCases(t *testing.T) {
	store := newMockUserStore()
	auth := New(store, "") // secretKey == "" default

	// Nil auth IsAuthenticated
	var nilAuth *Authenticator
	if !nilAuth.IsAuthenticated(nil) {
		t.Error("nil auth IsAuthenticated should return true")
	}

	app := fiber.New()
	app.Get("/auth/callback", auth.Callback)
	app.Get("/auth/login", auth.Login)
	app.Post("/auth/login", auth.Login)
	app.Get("/protected", auth.Middleware(), func(c *fiber.Ctx) error {
		return c.SendString(c.Locals("actor").(string))
	})
	app.Get("/check", func(c *fiber.Ctx) error {
		if auth.IsAuthenticated(c) {
			return c.SendString("ok")
		}
		return c.SendStatus(fiber.StatusUnauthorized)
	})

	// 1. Callback redirects to /
	reqCb := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)
	respCb, _ := app.Test(reqCb)
	if respCb.StatusCode != fiber.StatusFound || respCb.Header.Get("Location") != "/" {
		t.Errorf("Callback should redirect to /, got status %d, loc %s", respCb.StatusCode, respCb.Header.Get("Location"))
	}

	// 2. IsAuthenticated check without cookie
	reqCheck := httptest.NewRequest(http.MethodGet, "/check", nil)
	respCheck, _ := app.Test(reqCheck)
	if respCheck.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("check without cookie status = %d, want 401", respCheck.StatusCode)
	}

	// 3. GET /auth/login returns HTML form
	reqForm := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	respForm, _ := app.Test(reqForm)
	if respForm.StatusCode != fiber.StatusOK {
		t.Errorf("GET login status = %d, want 200", respForm.StatusCode)
	}
	body, _ := io.ReadAll(respForm.Body)
	if !strings.Contains(string(body), "<form") {
		t.Errorf("GET login body missing form: %s", string(body))
	}

	// 4. POST /auth/login with empty fields
	reqEmpty := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	respEmpty, _ := app.Test(reqEmpty)
	if respEmpty.StatusCode != fiber.StatusBadRequest {
		t.Errorf("POST login empty fields status = %d, want 400", respEmpty.StatusCode)
	}

	// 5. POST /auth/login with invalid credentials
	form := url.Values{"username": {"ghost"}, "password": {"badpass"}}
	reqBad := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
	reqBad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respBad, _ := app.Test(reqBad)
	if respBad.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("POST login bad credentials status = %d, want 401", respBad.StatusCode)
	}

	// 6. Middleware with Datastar-Request returns SSE event
	reqSSE := httptest.NewRequest(http.MethodGet, "/protected", nil)
	reqSSE.Header.Set("Datastar-Request", "true")
	respSSE, _ := app.Test(reqSSE)
	if respSSE.StatusCode != fiber.StatusOK {
		t.Errorf("SSE unauthed status = %d, want 200", respSSE.StatusCode)
	}
	sseBody, _ := io.ReadAll(respSSE.Body)
	if !strings.Contains(string(sseBody), "datastar-patch-elements") {
		t.Errorf("expected datastar event in body, got: %s", string(sseBody))
	}

	// 7. Unsign errors
	if _, ok := auth.unsign("nodot"); ok {
		t.Error("unsign without dot should fail")
	}
	if _, ok := auth.unsign("bad!b64.sig"); ok {
		t.Error("unsign bad payload should fail")
	}
	if _, ok := auth.unsign("cGF5bG9hZA.bad!b64"); ok {
		t.Error("unsign bad sig should fail")
	}

	// 8. VerifyPassword format errors
	if _, err := VerifyPassword("p", "$argon2id$v=19$badparams$salt$hash"); err == nil {
		t.Error("VerifyPassword bad params should fail")
	}
	if _, err := VerifyPassword("p", "$argon2id$v=19$m=65536,t=1,p=4$bad!salt$hash"); err == nil {
		t.Error("VerifyPassword bad salt should fail")
	}
	if _, err := VerifyPassword("p", "$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$bad!hash"); err == nil {
		t.Error("VerifyPassword bad hash should fail")
	}
}

func TestLocalAuth_RemainingBranches(t *testing.T) {
	auth := New(newMockUserStore(), "test-secret-key-that-is-long-enough")

	// 1. sanitizeReturnTo variants
	if res := sanitizeReturnTo(""); res != "/" {
		t.Errorf("expected '/', got %q", res)
	}
	if res := sanitizeReturnTo("//attacker.com"); res != "/" {
		t.Errorf("expected '/', got %q", res)
	}
	if res := sanitizeReturnTo("/valid/path"); res != "/valid/path" {
		t.Errorf("expected '/valid/path', got %q", res)
	}
	if res := sanitizeReturnTo("/path\\bad"); res != "/" {
		t.Errorf("expected '/', got %q", res)
	}
	if res := sanitizeReturnTo("http://attacker.com"); res != "/" {
		t.Errorf("expected '/', got %q", res)
	}

	// 2. unsign with mismatched HMAC signature
	validSigned := auth.sign([]byte("data"))
	parts := strings.Split(validSigned, ".")
	tampered := parts[0] + ".c29tZXJhbmRvbWhhc2h0aGF0ZG9lc25vdG1hdGNo"
	if _, ok := auth.unsign(tampered); ok {
		t.Error("expected unsign on tampered signature to return false")
	}

	// 3. readSession: signed non-JSON & expired session & email-only actor
	app := fiber.New()
	app.Use(auth.Middleware())
	app.Get("/actor", func(c *fiber.Ctx) error {
		return c.SendString(c.Locals("actor").(string))
	})

	// Signed non-JSON
	signedNonJSON := auth.sign([]byte("not json at all"))
	reqNonJSON := httptest.NewRequest(http.MethodGet, "/actor", nil)
	reqNonJSON.AddCookie(&http.Cookie{Name: sessionCookie, Value: signedNonJSON})
	respNonJSON, _ := app.Test(reqNonJSON)
	if respNonJSON.StatusCode != fiber.StatusFound {
		t.Errorf("status=%d, want 302 redirect for non-JSON session", respNonJSON.StatusCode)
	}

	// Expired session
	expData, _ := json.Marshal(sessionData{
		Username: "alice",
		Exp:      time.Now().Add(-1 * time.Hour).Unix(),
	})
	signedExp := auth.sign(expData)
	reqExp := httptest.NewRequest(http.MethodGet, "/actor", nil)
	reqExp.AddCookie(&http.Cookie{Name: sessionCookie, Value: signedExp})
	respExp, _ := app.Test(reqExp)
	if respExp.StatusCode != fiber.StatusFound {
		t.Errorf("status=%d, want 302 redirect for expired session", respExp.StatusCode)
	}

	// Email-only actor (Username == "")
	emailData, _ := json.Marshal(sessionData{
		Username: "",
		Email:    "alice@example.com",
		Exp:      time.Now().Add(1 * time.Hour).Unix(),
	})
	signedEmail := auth.sign(emailData)
	reqEmail := httptest.NewRequest(http.MethodGet, "/actor", nil)
	reqEmail.AddCookie(&http.Cookie{Name: sessionCookie, Value: signedEmail})
	respEmail, _ := app.Test(reqEmail)
	if respEmail.StatusCode != fiber.StatusOK {
		t.Errorf("status=%d, want 200 for email actor", respEmail.StatusCode)
	}
	bodyEmail, _ := io.ReadAll(respEmail.Body)
	if string(bodyEmail) != "alice@example.com" {
		t.Errorf("got actor=%q, want 'alice@example.com'", string(bodyEmail))
	}

	// 4. Login with nil store -> 503
	nilStoreAuth := New(nil, "secret")
	nilApp := fiber.New()
	nilApp.Post("/auth/login", nilStoreAuth.Login)
	form := url.Values{"username": {"user"}, "password": {"pass"}}
	reqNil := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
	reqNil.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	respNil, _ := nilApp.Test(reqNil)
	if respNil.StatusCode != fiber.StatusServiceUnavailable {
		t.Errorf("status=%d, want 503 for nil store login", respNil.StatusCode)
	}
}

