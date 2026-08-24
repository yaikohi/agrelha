// Package auth gates agrelha behind Zitadel OIDC (authorization-code flow).
// Single permitted identity: cfg.AllowedEmail. Everything except /healthz and the
// /auth/* endpoints requires a valid session.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"agrelha/internal/config"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gofiber/fiber/v2"
	"golang.org/x/oauth2"
)

const sessionCookie = "agrelha_session"

type Authenticator struct {
	cfg      *config.Config
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config

	mu       sync.Mutex
	sessions map[string]string // token -> email
}

func New(ctx context.Context, cfg *config.Config) (*Authenticator, error) {
	provider, err := oidc.NewProvider(ctx, cfg.OIDCIssuer)
	if err != nil {
		return nil, err
	}
	return &Authenticator{
		cfg:      cfg,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID}),
		oauth: oauth2.Config{
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			RedirectURL:  cfg.OIDCRedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		sessions: map[string]string{},
	}, nil
}

func token() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Login redirects to Zitadel. (TODO(step③): sign/verify `state` instead of a fixed value.)
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
	if claims.Email == "" || claims.Email != a.cfg.AllowedEmail {
		return fiber.NewError(fiber.StatusForbidden, "not authorized")
	}

	t := token()
	a.mu.Lock()
	a.sessions[t] = claims.Email
	a.mu.Unlock()

	c.Cookie(&fiber.Cookie{
		Name: sessionCookie, Value: t, HTTPOnly: true, Secure: true,
		SameSite: "Lax", Path: "/", Expires: time.Now().Add(12 * time.Hour),
	})
	return c.Redirect("/", fiber.StatusFound)
}

func (a *Authenticator) Logout(c *fiber.Ctx) error {
	if t := c.Cookies(sessionCookie); t != "" {
		a.mu.Lock()
		delete(a.sessions, t)
		a.mu.Unlock()
	}
	c.ClearCookie(sessionCookie)
	return c.Redirect("/auth/login", fiber.StatusFound)
}

// Middleware requires a valid session; stashes the actor email in locals.
func (a *Authenticator) Middleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		t := c.Cookies(sessionCookie)
		a.mu.Lock()
		email, ok := a.sessions[t]
		a.mu.Unlock()
		if !ok {
			return c.Redirect("/auth/login", fiber.StatusFound)
		}
		c.Locals("actor", email)
		return c.Next()
	}
}
