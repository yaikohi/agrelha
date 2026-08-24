// Package auth gates agrelha behind Zitadel OIDC (authorization-code flow).
// Single permitted identity: cfg.AllowedEmail. Everything except /healthz, /login
// and the /auth/* endpoints requires a valid session. Logout is RP-initiated
// (redirects to the IdP's end_session_endpoint) so the Zitadel session ends too.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/url"
	"sync"
	"time"

	"agrelha/internal/config"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gofiber/fiber/v2"
	"golang.org/x/oauth2"
)

const sessionCookie = "agrelha_session"

type session struct {
	email   string
	idToken string // raw ID token, used as id_token_hint on logout
}

type Authenticator struct {
	cfg      *config.Config
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config

	endSession string // IdP end_session_endpoint ("" if not advertised)
	postLogout string // where the IdP returns after logout

	mu       sync.Mutex
	sessions map[string]session // token -> session
}

func New(ctx context.Context, cfg *config.Config) (*Authenticator, error) {
	provider, err := oidc.NewProvider(ctx, cfg.OIDCIssuer)
	if err != nil {
		return nil, err
	}
	// end_session_endpoint isn't on oidc.Provider directly — pull it from discovery.
	var disco struct {
		EndSession string `json:"end_session_endpoint"`
	}
	_ = provider.Claims(&disco)

	// Return the user to /login (same origin as the redirect URL) after IdP logout.
	postLogout := "/login"
	if u, err := url.Parse(cfg.OIDCRedirectURL); err == nil && u.Host != "" {
		postLogout = u.Scheme + "://" + u.Host + "/login"
	}

	return &Authenticator{
		cfg:        cfg,
		provider:   provider,
		verifier:   provider.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID}),
		endSession: disco.EndSession,
		postLogout: postLogout,
		oauth: oauth2.Config{
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			RedirectURL:  cfg.OIDCRedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		sessions: map[string]session{},
	}, nil
}

func token() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Login redirects to Zitadel. (TODO: sign/verify `state` instead of a fixed value.)
func (a *Authenticator) Login(c *fiber.Ctx) error {
	return c.Redirect(a.oauth.AuthCodeURL("state-todo"), fiber.StatusFound)
}

// Callback exchanges the code, verifies the ID token, and enforces AllowedEmail.
func (a *Authenticator) Callback(c *fiber.Ctx) error {
	ctx := c.UserContext()
	oauth2Token, err := a.oauth.Exchange(ctx, c.Query("code"))
	if err != nil {
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
	var claims struct {
		Email string `json:"email"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "claims parse failed")
	}
	// Fall back to the UserInfo endpoint if the ID token carries no email
	// (i.e. "User Info inside ID Token" is disabled on the Zitadel app).
	if claims.Email == "" {
		if ui, err := a.provider.UserInfo(ctx, oauth2.StaticTokenSource(oauth2Token)); err == nil {
			claims.Email = ui.Email
		}
	}
	if claims.Email == "" || claims.Email != a.cfg.AllowedEmail {
		log.Printf("auth: rejected sign-in for email %q (allowed %q)", claims.Email, a.cfg.AllowedEmail)
		return fiber.NewError(fiber.StatusForbidden, "not authorized")
	}

	t := token()
	a.mu.Lock()
	a.sessions[t] = session{email: claims.Email, idToken: rawID}
	a.mu.Unlock()
	log.Printf("auth: %s signed in", claims.Email)

	c.Cookie(&fiber.Cookie{
		Name: sessionCookie, Value: t, HTTPOnly: true, Secure: true,
		SameSite: "Lax", Path: "/", Expires: time.Now().Add(12 * time.Hour),
	})
	return c.Redirect("/", fiber.StatusFound)
}

// Logout clears the local session and, when the IdP advertises an
// end_session_endpoint, redirects there to end the Zitadel session too
// (RP-initiated logout). Zitadel returns to postLogout afterwards.
func (a *Authenticator) Logout(c *fiber.Ctx) error {
	var idHint string
	if t := c.Cookies(sessionCookie); t != "" {
		a.mu.Lock()
		if s, ok := a.sessions[t]; ok {
			idHint = s.idToken
		}
		delete(a.sessions, t)
		a.mu.Unlock()
	}
	c.ClearCookie(sessionCookie)

	if a.endSession == "" {
		return c.Redirect("/login", fiber.StatusFound)
	}
	u, err := url.Parse(a.endSession)
	if err != nil {
		return c.Redirect("/login", fiber.StatusFound)
	}
	q := u.Query()
	q.Set("post_logout_redirect_uri", a.postLogout)
	q.Set("client_id", a.cfg.OIDCClientID)
	if idHint != "" {
		q.Set("id_token_hint", idHint)
	}
	u.RawQuery = q.Encode()
	return c.Redirect(u.String(), fiber.StatusFound)
}

// Middleware requires a valid session; stashes the actor email in locals.
func (a *Authenticator) Middleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		t := c.Cookies(sessionCookie)
		a.mu.Lock()
		s, ok := a.sessions[t]
		a.mu.Unlock()
		if !ok {
			return c.Redirect("/login", fiber.StatusFound)
		}
		c.Locals("actor", s.email)
		return c.Next()
	}
}
