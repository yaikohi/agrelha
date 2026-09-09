package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/gofiber/fiber/v2"

	"agrelha/internal/platform/config"
)

// mockIDP is an in-process, spec-compliant OIDC provider for tests.
type mockIDP struct {
	server       *httptest.Server
	key          *rsa.PrivateKey
	signingKey   *rsa.PrivateKey // used for token signing (can be swapped for rogue key)
	keyID        string
	clientID     string
	clientSecret string

	mu                sync.Mutex
	userEmail         string
	customNonce       string
	tokenExpiryOffset time.Duration
	failTokenEndpoint bool
	includeEndSession bool
	lastRequestedCode string
	lastNonce         string
	lastRedirectURI   string
}

func newMockIDP(t *testing.T) *mockIDP {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	idp := &mockIDP{
		key:               k,
		signingKey:        k,
		keyID:             "test-key-id-1",
		clientID:          "test-client-id",
		clientSecret:      "test-client-secret",
		userEmail:         "admin@example.com",
		tokenExpiryOffset: 1 * time.Hour,
		includeEndSession: true,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", idp.handleDiscovery)
	mux.HandleFunc("/jwks", idp.handleJWKS)
	mux.HandleFunc("/auth", idp.handleAuth)
	mux.HandleFunc("/token", idp.handleToken)
	mux.HandleFunc("/userinfo", idp.handleUserInfo)
	mux.HandleFunc("/logout", idp.handleLogout)

	idp.server = httptest.NewServer(mux)
	return idp
}

func (idp *mockIDP) Close() {
	idp.server.Close()
}

func (idp *mockIDP) Issuer() string {
	return idp.server.URL
}

func (idp *mockIDP) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	disc := map[string]interface{}{
		"issuer":                                idp.Issuer(),
		"authorization_endpoint":                idp.Issuer() + "/auth",
		"token_endpoint":                        idp.Issuer() + "/token",
		"jwks_uri":                              idp.Issuer() + "/jwks",
		"userinfo_endpoint":                     idp.Issuer() + "/userinfo",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "email", "profile"},
	}
	idp.mu.Lock()
	if idp.includeEndSession {
		disc["end_session_endpoint"] = idp.Issuer() + "/logout"
	}
	idp.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(disc)
}

func (idp *mockIDP) handleJWKS(w http.ResponseWriter, r *http.Request) {
	jwk := jose.JSONWebKey{
		Key:       &idp.key.PublicKey,
		KeyID:     idp.keyID,
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
	set := jose.JSONWebKeySet{
		Keys: []jose.JSONWebKey{jwk},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(set)
}

func (idp *mockIDP) handleAuth(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")
	nonce := q.Get("nonce")

	idp.mu.Lock()
	idp.lastNonce = nonce
	idp.lastRedirectURI = redirectURI
	code := "mock-auth-code-12345"
	idp.lastRequestedCode = code
	idp.mu.Unlock()

	target := fmt.Sprintf("%s?code=%s&state=%s", redirectURI, url.QueryEscape(code), url.QueryEscape(state))
	http.Redirect(w, r, target, http.StatusFound)
}

func (idp *mockIDP) handleToken(w http.ResponseWriter, r *http.Request) {
	idp.mu.Lock()
	fail := idp.failTokenEndpoint
	signingKey := idp.signingKey
	email := idp.userEmail
	expiryOffset := idp.tokenExpiryOffset
	customNonce := idp.customNonce
	lastNonce := idp.lastNonce
	idp.mu.Unlock()

	if fail {
		http.Error(w, `{"error":"server_error","error_description":"simulated idp token error"}`, http.StatusInternalServerError)
		return
	}

	nonce := lastNonce
	if customNonce != "" {
		nonce = customNonce
	}

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: signingKey},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", idp.keyID),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	now := time.Now()
	claims := map[string]interface{}{
		"iss":   idp.Issuer(),
		"sub":   "mock-user-sub-001",
		"aud":   idp.clientID,
		"exp":   now.Add(expiryOffset).Unix(),
		"iat":   now.Unix(),
		"email": email,
		"nonce": nonce,
	}

	rawIDToken, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"access_token": "mock-access-token-xyz",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     rawIDToken,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (idp *mockIDP) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	idp.mu.Lock()
	email := idp.userEmail
	idp.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"sub":   "mock-user-sub-001",
		"email": email,
	})
}

func (idp *mockIDP) handleLogout(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("post_logout_redirect_uri")
	if target == "" {
		target = "/"
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func setupTestApp(t *testing.T, idp *mockIDP, allowedEmail string) (*fiber.App, *Authenticator, *config.Config) {
	t.Helper()
	cfg := &config.Config{
		OIDCIssuer:       idp.Issuer(),
		OIDCClientID:     idp.clientID,
		OIDCClientSecret: idp.clientSecret,
		OIDCRedirectURL:  "http://localhost:8080/auth/callback",
		AllowedEmail:     allowedEmail,
	}

	a, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("auth.New failed: %v", err)
	}

	app := fiber.New()
	app.Get("/auth/login", a.Login)
	app.Get("/auth/callback", a.Callback)
	app.Get("/auth/logout", a.Logout)

	app.Get("/protected", a.Middleware(), func(c *fiber.Ctx) error {
		return c.SendString("protected: actor=" + c.Locals("actor").(string))
	})

	return app, a, cfg
}

func extractCookie(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func isCookieCleared(resp *http.Response, name string) bool {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			if c.Value == "" || c.MaxAge < 0 || (!c.Expires.IsZero() && c.Expires.Before(time.Now())) {
				return true
			}
		}
	}
	for _, raw := range resp.Header.Values("Set-Cookie") {
		if strings.Contains(raw, name+"=") {
			low := strings.ToLower(raw)
			if strings.Contains(low, "expires=") || strings.Contains(low, "max-age=") {
				return true
			}
		}
	}
	return false
}

// Follow 302 redirect from mock IdP authorize without following further
func performAuthorizeHop(t *testing.T, authURL string) string {
	t.Helper()
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(authURL)
	if err != nil {
		t.Fatalf("GET authURL failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 302 from IdP authorize, got %d: %s", resp.StatusCode, string(body))
	}
	return resp.Header.Get("Location")
}

func TestOIDCHappyPathAndSessionFlags(t *testing.T) {
	idp := newMockIDP(t)
	defer idp.Close()

	allowedEmail := "admin@example.com"
	idp.userEmail = allowedEmail
	app, _, _ := setupTestApp(t, idp, allowedEmail)

	// 1. Initiate Login
	loginReq := httptest.NewRequest(fiber.MethodGet, "/auth/login", nil)
	loginResp, err := app.Test(loginReq)
	if err != nil {
		t.Fatal(err)
	}
	if loginResp.StatusCode != fiber.StatusFound {
		t.Fatalf("expected 302 from /auth/login, got %d", loginResp.StatusCode)
	}

	authRedirect := loginResp.Header.Get("Location")
	if !strings.HasPrefix(authRedirect, idp.Issuer()+"/auth") {
		t.Fatalf("expected redirect to IdP authorize endpoint, got %s", authRedirect)
	}

	// Verify oidcCookie was set with proper flags
	oidcCookie := extractCookie(loginResp, "agrelha_oidc")
	if oidcCookie == nil {
		t.Fatal("expected agrelha_oidc cookie to be set")
	}
	if !oidcCookie.HttpOnly {
		t.Errorf("expected agrelha_oidc to be HttpOnly")
	}
	if oidcCookie.Path != "/" {
		t.Errorf("expected agrelha_oidc path to be /, got %s", oidcCookie.Path)
	}

	// 2. Perform authorize at IdP
	callbackRedirect := performAuthorizeHop(t, authRedirect)
	cbURL, err := url.Parse(callbackRedirect)
	if err != nil {
		t.Fatal(err)
	}
	code := cbURL.Query().Get("code")
	state := cbURL.Query().Get("state")
	if code == "" || state == "" {
		t.Fatalf("expected code and state in callback redirect, got: %s", callbackRedirect)
	}

	// 3. Callback to app
	cbReq := httptest.NewRequest(fiber.MethodGet, "/auth/callback?code="+code+"&state="+state, nil)
	cbReq.AddCookie(oidcCookie)
	cbResp, err := app.Test(cbReq)
	if err != nil {
		t.Fatal(err)
	}
	if cbResp.StatusCode != fiber.StatusFound {
		body, _ := io.ReadAll(cbResp.Body)
		t.Fatalf("expected 302 from /auth/callback, got %d: %s", cbResp.StatusCode, string(body))
	}
	if loc := cbResp.Header.Get("Location"); loc != "/" {
		t.Errorf("expected redirect to /, got %s", loc)
	}

	// 4. Verify session cookie flags
	sessionCookie := extractCookie(cbResp, "agrelha_session")
	if sessionCookie == nil {
		t.Fatal("expected agrelha_session cookie to be set")
	}
	if !sessionCookie.HttpOnly {
		t.Errorf("expected session cookie to be HttpOnly")
	}
	if sessionCookie.Path != "/" {
		t.Errorf("expected session cookie path to be /, got %s", sessionCookie.Path)
	}
	// Value should be HMAC-signed payload (payload.sig), not a raw unsigned JWT
	if !strings.Contains(sessionCookie.Value, ".") {
		t.Errorf("expected signed session value format payload.signature")
	}

	// Verify that oidcCookie was expired/cleared
	if !isCookieCleared(cbResp, "agrelha_oidc") {
		t.Errorf("expected agrelha_oidc to be expired/cleared on callback")
	}

	// 5. Access protected route with session cookie
	protReq := httptest.NewRequest(fiber.MethodGet, "/protected", nil)
	protReq.AddCookie(sessionCookie)
	protResp, err := app.Test(protReq)
	if err != nil {
		t.Fatal(err)
	}
	if protResp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 from protected route, got %d", protResp.StatusCode)
	}
	body, _ := io.ReadAll(protResp.Body)
	if string(body) != "protected: actor="+allowedEmail {
		t.Errorf("unexpected protected body: %s", string(body))
	}
}

func TestCSRFStateMismatchAndMissingState(t *testing.T) {
	idp := newMockIDP(t)
	defer idp.Close()

	app, _, _ := setupTestApp(t, idp, "admin@example.com")

	// 1. Missing cookie entirely
	req := httptest.NewRequest(fiber.MethodGet, "/auth/callback?code=somecode&state=somestate", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for missing login state cookie, got %d", resp.StatusCode)
	}

	// 2. Start login to get valid cookie
	loginReq := httptest.NewRequest(fiber.MethodGet, "/auth/login", nil)
	loginResp, err := app.Test(loginReq)
	if err != nil {
		t.Fatal(err)
	}
	oidcCookie := extractCookie(loginResp, "agrelha_oidc")

	// Callback with mismatched state
	reqMismatched := httptest.NewRequest(fiber.MethodGet, "/auth/callback?code=somecode&state=wrong-state-attacker", nil)
	reqMismatched.AddCookie(oidcCookie)
	respMismatched, err := app.Test(reqMismatched)
	if err != nil {
		t.Fatal(err)
	}
	if respMismatched.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for CSRF state mismatch, got %d", respMismatched.StatusCode)
	}
}

func TestForbiddenIdentity(t *testing.T) {
	idp := newMockIDP(t)
	defer idp.Close()

	app, _, _ := setupTestApp(t, idp, "allowed@example.com")
	// IdP returns an unauthorized email
	idp.userEmail = "unauthorized@evil.com"

	loginReq := httptest.NewRequest(fiber.MethodGet, "/auth/login", nil)
	loginResp, err := app.Test(loginReq)
	if err != nil {
		t.Fatal(err)
	}
	oidcCookie := extractCookie(loginResp, "agrelha_oidc")
	callbackRedirect := performAuthorizeHop(t, loginResp.Header.Get("Location"))
	cbURL, _ := url.Parse(callbackRedirect)

	cbReq := httptest.NewRequest(fiber.MethodGet, "/auth/callback?code="+cbURL.Query().Get("code")+"&state="+cbURL.Query().Get("state"), nil)
	cbReq.AddCookie(oidcCookie)
	cbResp, err := app.Test(cbReq)
	if err != nil {
		t.Fatal(err)
	}
	if cbResp.StatusCode != fiber.StatusForbidden {
		t.Errorf("expected 403 Forbidden for unauthorized email, got %d", cbResp.StatusCode)
	}
	if session := extractCookie(cbResp, "agrelha_session"); session != nil && session.Value != "" {
		t.Errorf("expected no session cookie issued for unauthorized identity")
	}
}

func TestTokenEndpointFailure(t *testing.T) {
	idp := newMockIDP(t)
	defer idp.Close()

	app, _, _ := setupTestApp(t, idp, "admin@example.com")
	idp.userEmail = "admin@example.com"

	loginReq := httptest.NewRequest(fiber.MethodGet, "/auth/login", nil)
	loginResp, err := app.Test(loginReq)
	if err != nil {
		t.Fatal(err)
	}
	oidcCookie := extractCookie(loginResp, "agrelha_oidc")
	callbackRedirect := performAuthorizeHop(t, loginResp.Header.Get("Location"))
	cbURL, _ := url.Parse(callbackRedirect)

	// Make token endpoint fail
	idp.failTokenEndpoint = true

	cbReq := httptest.NewRequest(fiber.MethodGet, "/auth/callback?code="+cbURL.Query().Get("code")+"&state="+cbURL.Query().Get("state"), nil)
	cbReq.AddCookie(oidcCookie)
	cbResp, err := app.Test(cbReq)
	if err != nil {
		t.Fatal(err)
	}
	if cbResp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("expected 401 on token exchange failure, got %d", cbResp.StatusCode)
	}
}

func TestOpenRedirectDefenseOnReturnTo(t *testing.T) {
	idp := newMockIDP(t)
	defer idp.Close()

	app, _, _ := setupTestApp(t, idp, "admin@example.com")
	idp.userEmail = "admin@example.com"

	maliciousTargets := []string{
		"https://evil.com",
		"http://evil.com/phish",
		"//evil.com",
		"/\\evil.com",
		"javascript:alert(1)",
	}

	for _, target := range maliciousTargets {
		loginReq := httptest.NewRequest(fiber.MethodGet, "/auth/login?returnTo="+url.QueryEscape(target), nil)
		loginResp, err := app.Test(loginReq)
		if err != nil {
			t.Fatal(err)
		}
		oidcCookie := extractCookie(loginResp, "agrelha_oidc")
		callbackRedirect := performAuthorizeHop(t, loginResp.Header.Get("Location"))
		cbURL, _ := url.Parse(callbackRedirect)

		cbReq := httptest.NewRequest(fiber.MethodGet, "/auth/callback?code="+cbURL.Query().Get("code")+"&state="+cbURL.Query().Get("state"), nil)
		cbReq.AddCookie(oidcCookie)
		cbResp, err := app.Test(cbReq)
		if err != nil {
			t.Fatal(err)
		}
		loc := cbResp.Header.Get("Location")
		if loc != "/" {
			t.Errorf("target %q allowed open redirect: got %q, want %q", target, loc, "/")
		}
	}

	// Legitimate internal relative redirect should be preserved
	loginReq := httptest.NewRequest(fiber.MethodGet, "/auth/login?returnTo=/minecraft/server/2", nil)
	loginResp, err := app.Test(loginReq)
	if err != nil {
		t.Fatal(err)
	}
	oidcCookie := extractCookie(loginResp, "agrelha_oidc")
	callbackRedirect := performAuthorizeHop(t, loginResp.Header.Get("Location"))
	cbURL, _ := url.Parse(callbackRedirect)

	cbReq := httptest.NewRequest(fiber.MethodGet, "/auth/callback?code="+cbURL.Query().Get("code")+"&state="+cbURL.Query().Get("state"), nil)
	cbReq.AddCookie(oidcCookie)
	cbResp, err := app.Test(cbReq)
	if err != nil {
		t.Fatal(err)
	}
	if loc := cbResp.Header.Get("Location"); loc != "/minecraft/server/2" {
		t.Errorf("legitimate returnTo preserved: got %q, want %q", loc, "/minecraft/server/2")
	}
}

func TestDatastarSSETrapOnExpiredSession(t *testing.T) {
	idp := newMockIDP(t)
	defer idp.Close()

	app, _, _ := setupTestApp(t, idp, "admin@example.com")

	// Case 1: Standard browser navigation when unauthenticated -> 302 redirect
	reqNav := httptest.NewRequest(fiber.MethodGet, "/protected", nil)
	reqNav.Header.Set("Accept", "text/html,application/xhtml+xml")
	respNav, err := app.Test(reqNav)
	if err != nil {
		t.Fatal(err)
	}
	if respNav.StatusCode != fiber.StatusFound {
		t.Errorf("expected 302 for browser page navigation, got %d", respNav.StatusCode)
	}
	if loc := respNav.Header.Get("Location"); !strings.HasPrefix(loc, "/auth/login?returnTo=") {
		t.Errorf("expected redirect to /auth/login with returnTo, got %s", loc)
	}

	// Case 2: Datastar SSE request (via Datastar-Request header) -> MUST NOT 302
	reqDS := httptest.NewRequest(fiber.MethodGet, "/protected", nil)
	reqDS.Header.Set("Datastar-Request", "true")
	respDS, err := app.Test(reqDS)
	if err != nil {
		t.Fatal(err)
	}
	if respDS.StatusCode != fiber.StatusOK {
		t.Fatalf("Datastar request must NOT receive 302, got status %d", respDS.StatusCode)
	}
	if ct := respDS.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream for Datastar unauthenticated response, got %q", ct)
	}
	bodyDS, _ := io.ReadAll(respDS.Body)
	content := string(bodyDS)
	if !strings.Contains(content, "event: datastar-patch-elements") {
		t.Errorf("missing datastar-patch-elements event in SSE stream: %s", content)
	}
	if !strings.Contains(content, "<script>window.location.href =") {
		t.Errorf("missing window.location script in Datastar SSE stream: %s", content)
	}

	// Case 3: EventSource / SSE Accept header -> MUST NOT 302
	reqSSE := httptest.NewRequest(fiber.MethodGet, "/protected", nil)
	reqSSE.Header.Set("Accept", "text/event-stream")
	respSSE, err := app.Test(reqSSE)
	if err != nil {
		t.Fatal(err)
	}
	if respSSE.StatusCode != fiber.StatusOK {
		t.Fatalf("SSE Accept request must NOT receive 302, got status %d", respSSE.StatusCode)
	}
	bodySSE, _ := io.ReadAll(respSSE.Body)
	if !strings.Contains(string(bodySSE), "<script>window.location.href =") {
		t.Errorf("missing window.location script in SSE Accept stream: %s", string(bodySSE))
	}
}

func TestRPInitiatedLogoutAndSessionExpiry(t *testing.T) {
	idp := newMockIDP(t)
	defer idp.Close()

	app, authObj, _ := setupTestApp(t, idp, "admin@example.com")
	idp.userEmail = "admin@example.com"

	// Perform login to establish session
	loginReq := httptest.NewRequest(fiber.MethodGet, "/auth/login", nil)
	loginResp, _ := app.Test(loginReq)
	oidcCookie := extractCookie(loginResp, "agrelha_oidc")
	callbackRedirect := performAuthorizeHop(t, loginResp.Header.Get("Location"))
	cbURL, _ := url.Parse(callbackRedirect)

	cbReq := httptest.NewRequest(fiber.MethodGet, "/auth/callback?code="+cbURL.Query().Get("code")+"&state="+cbURL.Query().Get("state"), nil)
	cbReq.AddCookie(oidcCookie)
	cbResp, _ := app.Test(cbReq)
	sessionCookie := extractCookie(cbResp, "agrelha_session")
	if sessionCookie == nil {
		t.Fatal("session cookie missing")
	}

	// Call logout with active session
	logoutReq := httptest.NewRequest(fiber.MethodGet, "/auth/logout", nil)
	logoutReq.AddCookie(sessionCookie)
	logoutResp, err := app.Test(logoutReq)
	if err != nil {
		t.Fatal(err)
	}
	if logoutResp.StatusCode != fiber.StatusFound {
		t.Fatalf("expected 302 from logout, got %d", logoutResp.StatusCode)
	}
	redirectLoc := logoutResp.Header.Get("Location")
	if !strings.HasPrefix(redirectLoc, authObj.endSession) {
		t.Errorf("expected redirect to IdP end_session_endpoint, got %s", redirectLoc)
	}
	if !strings.Contains(redirectLoc, "post_logout_redirect_uri=") {
		t.Errorf("expected post_logout_redirect_uri in IdP logout redirect, got %s", redirectLoc)
	}
	if !strings.Contains(redirectLoc, "id_token_hint=") {
		t.Errorf("expected id_token_hint in IdP logout redirect, got %s", redirectLoc)
	}

	// Verify cookies cleared
	if !isCookieCleared(logoutResp, "agrelha_session") {
		t.Errorf("expected agrelha_session to be expired on logout")
	}
}

func TestTokenSignedByWrongKeyRejected(t *testing.T) {
	idp := newMockIDP(t)
	defer idp.Close()

	app, _, _ := setupTestApp(t, idp, "admin@example.com")
	idp.userEmail = "admin@example.com"

	loginReq := httptest.NewRequest(fiber.MethodGet, "/auth/login", nil)
	loginResp, _ := app.Test(loginReq)
	oidcCookie := extractCookie(loginResp, "agrelha_oidc")
	callbackRedirect := performAuthorizeHop(t, loginResp.Header.Get("Location"))
	cbURL, _ := url.Parse(callbackRedirect)

	// Mint another rogue keypair not in IdP's JWKS
	rogueKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp.signingKey = rogueKey

	cbReq := httptest.NewRequest(fiber.MethodGet, "/auth/callback?code="+cbURL.Query().Get("code")+"&state="+cbURL.Query().Get("state"), nil)
	cbReq.AddCookie(oidcCookie)
	cbResp, err := app.Test(cbReq)
	if err != nil {
		t.Fatal(err)
	}
	if cbResp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for token signed by untrusted key, got %d", cbResp.StatusCode)
	}
}

func TestNonceMismatchRejected(t *testing.T) {
	idp := newMockIDP(t)
	defer idp.Close()

	app, _, _ := setupTestApp(t, idp, "admin@example.com")
	idp.userEmail = "admin@example.com"

	loginReq := httptest.NewRequest(fiber.MethodGet, "/auth/login", nil)
	loginResp, _ := app.Test(loginReq)
	oidcCookie := extractCookie(loginResp, "agrelha_oidc")
	callbackRedirect := performAuthorizeHop(t, loginResp.Header.Get("Location"))
	cbURL, _ := url.Parse(callbackRedirect)

	// Cause nonce mismatch
	idp.customNonce = "wrong-tampered-nonce"

	cbReq := httptest.NewRequest(fiber.MethodGet, "/auth/callback?code="+cbURL.Query().Get("code")+"&state="+cbURL.Query().Get("state"), nil)
	cbReq.AddCookie(oidcCookie)
	cbResp, err := app.Test(cbReq)
	if err != nil {
		t.Fatal(err)
	}
	if cbResp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for nonce mismatch, got %d", cbResp.StatusCode)
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	idp := newMockIDP(t)
	defer idp.Close()

	app, _, _ := setupTestApp(t, idp, "admin@example.com")
	idp.userEmail = "admin@example.com"

	// Token generated as expired 1 hour ago
	idp.tokenExpiryOffset = -1 * time.Hour

	loginReq := httptest.NewRequest(fiber.MethodGet, "/auth/login", nil)
	loginResp, _ := app.Test(loginReq)
	oidcCookie := extractCookie(loginResp, "agrelha_oidc")
	callbackRedirect := performAuthorizeHop(t, loginResp.Header.Get("Location"))
	cbURL, _ := url.Parse(callbackRedirect)

	cbReq := httptest.NewRequest(fiber.MethodGet, "/auth/callback?code="+cbURL.Query().Get("code")+"&state="+cbURL.Query().Get("state"), nil)
	cbReq.AddCookie(oidcCookie)
	cbResp, err := app.Test(cbReq)
	if err != nil {
		t.Fatal(err)
	}
	if cbResp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for expired token, got %d", cbResp.StatusCode)
	}
}

func TestMultipleAllowedEmailsAuthorized(t *testing.T) {
	idp := newMockIDP(t)
	defer idp.Close()

	// Configure multiple admin emails
	app, _, _ := setupTestApp(t, idp, "alice@example.com, bob@example.com, charlie@example.com")

	// 1. Alice is allowed
	idp.userEmail = "alice@example.com"
	loginReq := httptest.NewRequest(fiber.MethodGet, "/auth/login", nil)
	loginResp, _ := app.Test(loginReq)
	oidcCookie := extractCookie(loginResp, "agrelha_oidc")
	callbackRedirect := performAuthorizeHop(t, loginResp.Header.Get("Location"))
	cbURL, _ := url.Parse(callbackRedirect)

	cbReq := httptest.NewRequest(fiber.MethodGet, "/auth/callback?code="+cbURL.Query().Get("code")+"&state="+cbURL.Query().Get("state"), nil)
	cbReq.AddCookie(oidcCookie)
	cbResp, err := app.Test(cbReq)
	if err != nil {
		t.Fatal(err)
	}
	if cbResp.StatusCode != fiber.StatusFound {
		t.Fatalf("expected 302 for allowed email Alice, got %d", cbResp.StatusCode)
	}

	// 2. Bob is allowed
	idp.userEmail = "bob@example.com"
	loginReq = httptest.NewRequest(fiber.MethodGet, "/auth/login", nil)
	loginResp, _ = app.Test(loginReq)
	oidcCookie = extractCookie(loginResp, "agrelha_oidc")
	callbackRedirect = performAuthorizeHop(t, loginResp.Header.Get("Location"))
	cbURL, _ = url.Parse(callbackRedirect)

	cbReq = httptest.NewRequest(fiber.MethodGet, "/auth/callback?code="+cbURL.Query().Get("code")+"&state="+cbURL.Query().Get("state"), nil)
	cbReq.AddCookie(oidcCookie)
	cbResp, err = app.Test(cbReq)
	if err != nil {
		t.Fatal(err)
	}
	if cbResp.StatusCode != fiber.StatusFound {
		t.Fatalf("expected 302 for allowed email Bob, got %d", cbResp.StatusCode)
	}

	// 3. Eve is rejected
	idp.userEmail = "eve@example.com"
	loginReq = httptest.NewRequest(fiber.MethodGet, "/auth/login", nil)
	loginResp, _ = app.Test(loginReq)
	oidcCookie = extractCookie(loginResp, "agrelha_oidc")
	callbackRedirect = performAuthorizeHop(t, loginResp.Header.Get("Location"))
	cbURL, _ = url.Parse(callbackRedirect)

	cbReq = httptest.NewRequest(fiber.MethodGet, "/auth/callback?code="+cbURL.Query().Get("code")+"&state="+cbURL.Query().Get("state"), nil)
	cbReq.AddCookie(oidcCookie)
	cbResp, err = app.Test(cbReq)
	if err != nil {
		t.Fatal(err)
	}
	if cbResp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for unlisted email Eve, got %d", cbResp.StatusCode)
	}
}
