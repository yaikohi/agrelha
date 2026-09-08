package auth

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

	"agrelha/internal/config"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gofiber/fiber/v2"
	"golang.org/x/oauth2"
)

const (
	sessionCookie = "agrelha_session"
	oidcCookie    = "agrelha_oidc"
	sessionTTL    = 12 * time.Hour
	loginTTL      = 10 * time.Minute
)

type Authenticator struct {
	cfg      *config.Config
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config
	key      []byte

	endSession string
	postLogout string
}

type sessionData struct {
	Email   string `json:"email"`
	IDToken string `json:"idt"`
	Exp     int64  `json:"exp"`
}

func New(ctx context.Context, cfg *config.Config) (*Authenticator, error) {
	provider, err := oidc.NewProvider(ctx, cfg.OIDCIssuer)
	if err != nil {
		return nil, err
	}
	var disco struct {
		EndSession string `json:"end_session_endpoint"`
	}
	_ = provider.Claims(&disco)

	postLogout := "/login"
	if u, err := url.Parse(cfg.OIDCRedirectURL); err == nil && u.Host != "" {
		postLogout = u.Scheme + "://" + u.Host + "/login"
	}

	sum := sha256.Sum256([]byte("agrelha-session-v1:" + cfg.OIDCClientSecret))

	return &Authenticator{
		cfg:        cfg,
		provider:   provider,
		verifier:   provider.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID}),
		key:        sum[:],
		endSession: disco.EndSession,
		postLogout: postLogout,
		oauth: oauth2.Config{
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			RedirectURL:  cfg.OIDCRedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
	}, nil
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

func (a *Authenticator) readSession(c *fiber.Ctx) (sessionData, bool) {
	payload, ok := a.unsign(c.Cookies(sessionCookie))
	if !ok {
		return sessionData{}, false
	}
	var s sessionData
	if err := json.Unmarshal(payload, &s); err != nil {
		return sessionData{}, false
	}
	if time.Now().Unix() >= s.Exp {
		return sessionData{}, false
	}
	return s, true
}

func (a *Authenticator) Login(c *fiber.Ctx) error {
	state, nonce := randHex(16), randHex(16)
	c.Cookie(&fiber.Cookie{
		Name: oidcCookie, Value: a.sign([]byte(state + ":" + nonce)),
		HTTPOnly: true, Secure: true, SameSite: "Lax", Path: "/",
		Expires: time.Now().Add(loginTTL),
	})
	return c.Redirect(a.oauth.AuthCodeURL(state, oidc.Nonce(nonce)), fiber.StatusFound)
}

func (a *Authenticator) Callback(c *fiber.Ctx) error {
	ctx := c.UserContext()

	payload, ok := a.unsign(c.Cookies(oidcCookie))
	c.ClearCookie(oidcCookie)
	if !ok {
		return fiber.NewError(fiber.StatusBadRequest, "missing or bad login state")
	}
	wantState, wantNonce, _ := strings.Cut(string(payload), ":")
	if subtle.ConstantTimeCompare([]byte(wantState), []byte(c.Query("state"))) != 1 {
		return fiber.NewError(fiber.StatusBadRequest, "state mismatch")
	}

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
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(wantNonce)) != 1 {
		return fiber.NewError(fiber.StatusUnauthorized, "nonce mismatch")
	}

	var claims struct {
		Email string `json:"email"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "claims parse failed")
	}
	if claims.Email == "" {
		if ui, err := a.provider.UserInfo(ctx, oauth2.StaticTokenSource(oauth2Token)); err == nil {
			claims.Email = ui.Email
		}
	}
	if claims.Email == "" || claims.Email != a.cfg.AllowedEmail {
		slog.Warn("auth: sign-in rejected", "email", claims.Email, "allowed", a.cfg.AllowedEmail)
		return fiber.NewError(fiber.StatusForbidden, "not authorized")
	}

	data, _ := json.Marshal(sessionData{
		Email: claims.Email, IDToken: rawID, Exp: time.Now().Add(sessionTTL).Unix(),
	})
	c.Cookie(&fiber.Cookie{
		Name: sessionCookie, Value: a.sign(data), HTTPOnly: true, Secure: true,
		SameSite: "Lax", Path: "/", Expires: time.Now().Add(sessionTTL),
	})
	slog.Info("auth: signed in", "email", claims.Email)
	return c.Redirect("/", fiber.StatusFound)
}

func (a *Authenticator) Logout(c *fiber.Ctx) error {
	s, _ := a.readSession(c)
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
	if s.IDToken != "" {
		q.Set("id_token_hint", s.IDToken)
	}
	u.RawQuery = q.Encode()
	return c.Redirect(u.String(), fiber.StatusFound)
}

func (a *Authenticator) IsAuthenticated(c *fiber.Ctx) bool {
	if a == nil {
		return true
	}
	_, ok := a.readSession(c)
	return ok
}

func (a *Authenticator) Middleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		s, ok := a.readSession(c)
		if !ok {
			return c.Redirect("/auth/login", fiber.StatusFound)
		}
		c.Locals("actor", s.Email)
		return c.Next()
	}
}
