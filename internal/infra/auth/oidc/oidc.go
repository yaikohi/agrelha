package oidc

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"

	coreosOIDC "github.com/coreos/go-oidc/v3/oidc"
	"github.com/gofiber/fiber/v2"
	"golang.org/x/oauth2"
)

const (
	sessionCookie = "agrelha_session"
	oidcCookie    = "agrelha_oidc"
	sessionTTL    = 2 * time.Hour
	loginTTL      = 10 * time.Minute
	rolesClaim    = "urn:zitadel:iam:org:project:roles"
)

type Config struct {
	Issuer        string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	PostLogoutURL string
	ProjectID     string
}

type SignInHook func(ctx context.Context, id domain.Identity) error

type Authenticator struct {
	cfg      Config
	provider *coreosOIDC.Provider
	verifier *coreosOIDC.IDTokenVerifier
	oauth    oauth2.Config
	key      []byte
	sessions ports.Sessions
	onSignIn SignInHook

	endSession string
	postLogout string
	isDev      bool
}

var _ ports.Auth = (*Authenticator)(nil)

type Option func(*Authenticator)

func WithSessions(s ports.Sessions) Option {
	return func(a *Authenticator) { a.sessions = s }
}

func WithOnSignIn(fn SignInHook) Option {
	return func(a *Authenticator) { a.onSignIn = fn }
}

func postLogoutURL(cfg Config) string {
	if cfg.PostLogoutURL != "" {
		return cfg.PostLogoutURL
	}
	if u, err := url.Parse(cfg.RedirectURL); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host + "/"
	}
	return "/"
}

func New(ctx context.Context, cfg Config, opts ...Option) (*Authenticator, error) {
	provider, err := coreosOIDC.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, err
	}
	var disco struct {
		EndSession string `json:"end_session_endpoint"`
	}
	_ = provider.Claims(&disco)

	sum := sha256.Sum256([]byte("agrelha-session-v1:" + cfg.ClientSecret))

	scopes := []string{coreosOIDC.ScopeOpenID, "email", "profile"}
	if cfg.ProjectID != "" {
		scopes = append(scopes, "urn:zitadel:iam:org:project:id:"+cfg.ProjectID+":aud")
	}

	a := &Authenticator{
		cfg:        cfg,
		provider:   provider,
		verifier:   provider.Verifier(&coreosOIDC.Config{ClientID: cfg.ClientID}),
		key:        sum[:],
		endSession: disco.EndSession,
		postLogout: postLogoutURL(cfg),
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       scopes,
		},
	}
	for _, o := range opts {
		o(a)
	}
	return a, nil
}

func NewDev(cfg Config, opts ...Option) *Authenticator {
	sum := sha256.Sum256([]byte("agrelha-dev-secret-key-v1"))
	a := &Authenticator{
		cfg:        cfg,
		key:        sum[:],
		postLogout: postLogoutURL(cfg),
		isDev:      true,
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *Authenticator) sign(payload []byte) string {
	mac := hmac.New(sha256.New, a.key)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *Authenticator) unsign(v string) ([]byte, bool) {
	parts := strings.SplitN(v, ".", 2)
	if len(parts) != 2 {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	mac := hmac.New(sha256.New, a.key)
	mac.Write(payload)
	if subtle.ConstantTimeCompare(sig, mac.Sum(nil)) != 1 {
		return nil, false
	}
	return payload, true
}

func (a *Authenticator) secure() bool {
	return !strings.HasPrefix(a.cfg.RedirectURL, "http://")
}

func (a *Authenticator) setSessionCookie(c *fiber.Ctx, id string, expires time.Time) {
	c.Cookie(&fiber.Cookie{
		Name:     sessionCookie,
		Value:    a.sign([]byte(id)),
		HTTPOnly: true,
		Secure:   a.secure(),
		SameSite: "Lax",
		Path:     "/",
		Expires:  expires,
	})
}

func (a *Authenticator) clearCookie(c *fiber.Ctx, name string) {
	c.Cookie(&fiber.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Expires:  time.Now().Add(-24 * time.Hour),
		MaxAge:   -1,
		Secure:   a.secure(),
		HTTPOnly: true,
		SameSite: "Lax",
	})
}

func (a *Authenticator) sessionID(c *fiber.Ctx) (string, bool) {
	payload, ok := a.unsign(c.Cookies(sessionCookie))
	if !ok || len(payload) == 0 {
		return "", false
	}
	return string(payload), true
}

func (a *Authenticator) startSession(c *fiber.Ctx, id domain.Identity, rawIDToken string) error {
	if a.sessions == nil {
		return fiber.NewError(fiber.StatusInternalServerError, "no session store configured")
	}
	ctx := c.UserContext()
	if a.onSignIn != nil {
		if err := a.onSignIn(ctx, id); err != nil {
			slog.Error("auth: record sign-in failed", "subject", id.Subject, "err", err)
		}
	}
	now := time.Now()
	expires := now.Add(sessionTTL)
	sid := randHex(32)
	if err := a.sessions.CreateSession(ctx, ports.Session{
		ID:        sid,
		Subject:   id.Subject,
		Email:     id.Email,
		Name:      id.Name,
		Roles:     id.Roles,
		IDToken:   rawIDToken,
		CreatedAt: now,
		ExpiresAt: expires,
	}); err != nil {
		return err
	}
	a.setSessionCookie(c, sid, expires)
	return nil
}

func sanitizeReturnTo(target string) string {
	if target == "" {
		return "/"
	}
	if strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "//") && !strings.Contains(target, "\\") {
		if u, err := url.Parse(target); err == nil && u.Host == "" && u.Scheme == "" {
			return target
		}
	}
	return "/"
}

func (a *Authenticator) Login(c *fiber.Ctx) error {
	returnTo := sanitizeReturnTo(c.Query("returnTo"))
	if a.isDev {
		id := domain.Identity{
			Subject: "dev-admin",
			Email:   "admin@local.dev",
			Name:    "Dev Admin",
			Roles:   []domain.Role{domain.RoleAdmin, domain.RoleUser},
		}
		if err := a.startSession(c, id, "dev-mock-id-token"); err != nil {
			return err
		}
		slog.Warn("auth: signed in (dev mode)", "email", id.Email)
		return c.Redirect(returnTo, fiber.StatusFound)
	}

	state, nonce := randHex(16), randHex(16)
	c.Cookie(&fiber.Cookie{
		Name: oidcCookie, Value: a.sign([]byte(state + ":" + nonce + ":" + returnTo)),
		HTTPOnly: true, Secure: a.secure(), SameSite: "Lax", Path: "/",
		Expires: time.Now().Add(loginTTL),
	})
	return c.Redirect(a.oauth.AuthCodeURL(state, coreosOIDC.Nonce(nonce)), fiber.StatusFound)
}

func (a *Authenticator) Callback(c *fiber.Ctx) error {
	if a.isDev {
		return c.Redirect("/", fiber.StatusFound)
	}
	ctx := c.UserContext()

	payload, ok := a.unsign(c.Cookies(oidcCookie))
	a.clearCookie(c, oidcCookie)
	if !ok {
		return fiber.NewError(fiber.StatusBadRequest, "missing or bad login state")
	}
	parts := strings.Split(string(payload), ":")
	if len(parts) < 2 {
		return fiber.NewError(fiber.StatusBadRequest, "malformed login state")
	}
	wantState, wantNonce := parts[0], parts[1]
	returnTo := "/"
	if len(parts) >= 3 {
		returnTo = sanitizeReturnTo(parts[2])
	}
	if subtle.ConstantTimeCompare([]byte(wantState), []byte(c.Query("state"))) != 1 {
		return fiber.NewError(fiber.StatusBadRequest, "state mismatch")
	}

	oauth2Token, err := a.oauth.Exchange(ctx, c.Query("code"))
	if err != nil {
		slog.Warn("auth: token exchange failed", "err", err)
		return fiber.NewError(fiber.StatusUnauthorized, "token exchange failed")
	}
	rawID, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "no id_token")
	}
	idToken, err := a.verifier.Verify(ctx, rawID)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "id_token verify failed")
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(wantNonce)) != 1 {
		return fiber.NewError(fiber.StatusUnauthorized, "nonce mismatch")
	}

	var claims struct {
		Email string                     `json:"email"`
		Name  string                     `json:"name"`
		Roles map[string]json.RawMessage `json:"urn:zitadel:iam:org:project:roles"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "claims parse failed")
	}
	if claims.Email == "" {
		if ui, err := a.provider.UserInfo(ctx, oauth2.StaticTokenSource(oauth2Token)); err == nil {
			claims.Email = ui.Email
		}
	}

	id := domain.Identity{Subject: idToken.Subject, Email: claims.Email, Name: claims.Name}
	for k := range claims.Roles {
		id.Roles = append(id.Roles, domain.Role(k))
	}
	if !id.MaySignIn() {
		slog.Warn("auth: sign-in rejected, no agrelha role",
			"subject", id.Subject, "email", id.Email, "roles", len(claims.Roles))
		return fiber.NewError(fiber.StatusForbidden, "not authorized")
	}

	if err := a.startSession(c, id, rawID); err != nil {
		return err
	}
	slog.Info("auth: signed in", "email", id.Email, "roles", id.Roles)
	return c.Redirect(returnTo, fiber.StatusFound)
}

func (a *Authenticator) Logout(c *fiber.Ctx) error {
	var idTokenHint string
	if sid, ok := a.sessionID(c); ok && a.sessions != nil {
		if s, err := a.sessions.LoadSession(c.UserContext(), sid); err == nil && s != nil {
			idTokenHint = s.IDToken
		}
		if err := a.sessions.DeleteSession(c.UserContext(), sid); err != nil {
			slog.Error("auth: delete session failed", "err", err)
		}
	}

	a.clearCookie(c, sessionCookie)
	a.clearCookie(c, oidcCookie)

	if a.isDev || a.endSession == "" {
		return c.Redirect("/", fiber.StatusFound)
	}
	u, err := url.Parse(a.endSession)
	if err != nil {
		return c.Redirect("/", fiber.StatusFound)
	}
	q := u.Query()
	q.Set("post_logout_redirect_uri", a.postLogout)
	q.Set("client_id", a.cfg.ClientID)
	if idTokenHint != "" {
		q.Set("id_token_hint", idTokenHint)
	}
	u.RawQuery = q.Encode()
	return c.Redirect(u.String(), fiber.StatusFound)
}

func (a *Authenticator) Identify(c *fiber.Ctx) (domain.Identity, bool) {
	if a == nil || a.sessions == nil {
		return domain.Identity{}, false
	}
	sid, ok := a.sessionID(c)
	if !ok {
		return domain.Identity{}, false
	}
	s, err := a.sessions.LoadSession(c.UserContext(), sid)
	if err != nil || s == nil {
		return domain.Identity{}, false
	}
	if time.Until(s.ExpiresAt) < sessionTTL/2 {
		expires := time.Now().Add(sessionTTL)
		if err := a.sessions.TouchSession(c.UserContext(), sid, expires); err == nil {
			a.setSessionCookie(c, sid, expires)
		}
	}
	return domain.Identity{
		Subject: s.Subject,
		Email:   s.Email,
		Name:    s.Name,
		Roles:   s.Roles,
	}, true
}
