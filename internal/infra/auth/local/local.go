package local

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"agrelha/internal/ports"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/argon2"
)

const (
	argonMemory      = 64 * 1024 // 64 MB
	argonIterations  = 1
	argonParallelism = 4
	argonSaltLen     = 16
	argonKeyLen      = 32

	sessionCookie = "agrelha_session"
	sessionTTL    = 12 * time.Hour
)

// HashPassword hashes a plaintext password using Argon2id.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonIterations, argonParallelism, b64Salt, b64Hash), nil
}

// VerifyPassword checks whether a plaintext password matches an Argon2id encoded hash.
func VerifyPassword(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("invalid argon2id hash format")
	}

	var mem, iter uint32
	var threads uint8
	_, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &iter, &threads)
	if err != nil {
		return false, fmt.Errorf("parse argon2id params: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("decode salt: %w", err)
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("decode hash: %w", err)
	}

	computedHash := argon2.IDKey([]byte(password), salt, iter, mem, threads, uint32(len(expectedHash)))

	if subtle.ConstantTimeCompare(computedHash, expectedHash) == 1 {
		return true, nil
	}
	return false, nil
}

type sessionData struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Exp      int64  `json:"exp"`
}

// Authenticator implements ports.Auth using local Argon2id credentials and HMAC session cookies.
type Authenticator struct {
	store    ports.UserStore
	key      []byte
	isSecure bool
}

var _ ports.Auth = (*Authenticator)(nil)

// Option configures a local Authenticator.
type Option func(*Authenticator)

// WithSecureCookie sets whether the Secure cookie flag is required.
func WithSecureCookie(secure bool) Option {
	return func(a *Authenticator) {
		a.isSecure = secure
	}
}

// New creates a new local Authenticator.
func New(store ports.UserStore, secretKey string, opts ...Option) *Authenticator {
	if secretKey == "" {
		secretKey = "agrelha-local-auth-secret-key-v1"
	}
	sum := sha256.Sum256([]byte("agrelha-local-session-v1:" + secretKey))
	a := &Authenticator{
		store: store,
		key:   sum[:],
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
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
			loginTarget := "/auth/login"
			if target := sanitizeReturnTo(c.OriginalURL()); target != "/" {
				loginTarget += "?returnTo=" + url.QueryEscape(target)
			}

			if c.Get("Datastar-Request") == "true" || strings.Contains(c.Get("Accept"), "text/event-stream") {
				c.Set("Content-Type", "text/event-stream")
				c.Set("Cache-Control", "no-cache")
				c.Set("Connection", "keep-alive")
				msg := fmt.Sprintf("event: datastar-patch-elements\ndata: mode append\ndata: selector body\ndata: elements <script>window.location.href = %q</script>\n\n", loginTarget)
				return c.SendString(msg)
			}
			return c.Redirect(loginTarget, fiber.StatusFound)
		}
		actor := s.Username
		if actor == "" {
			actor = s.Email
		}
		c.Locals("actor", actor)
		return c.Next()
	}
}

func (a *Authenticator) Login(c *fiber.Ctx) error {
	returnTo := sanitizeReturnTo(c.Query("returnTo"))

	if c.Method() == fiber.MethodGet {
		// Provide simple fallback login HTML for non-OIDC local setups
		html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>Sign In - agrelha</title>
  <style>
    body { font-family: ui-sans-serif, system-ui, sans-serif; background: #09090b; color: #f4f4f5; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; }
    .card { background: #18181b; padding: 2rem; border-radius: 0.5rem; width: 320px; border: 1px solid #27272a; }
    h1 { font-size: 1.25rem; margin-top: 0; margin-bottom: 1.5rem; }
    input { width: 100%%; padding: 0.5rem; margin-bottom: 1rem; border-radius: 0.25rem; border: 1px solid #3f3f46; background: #27272a; color: #fff; box-sizing: border-box; }
    button { width: 100%%; padding: 0.5rem; border-radius: 0.25rem; border: none; background: #10b981; color: #000; font-weight: bold; cursor: pointer; }
    button:hover { background: #059669; }
  </style>
</head>
<body>
  <div class="card">
    <h1>Sign In to agrelha</h1>
    <form method="POST" action="/auth/login?returnTo=%s">
      <input type="text" name="username" placeholder="Username" required autofocus />
      <input type="password" name="password" placeholder="Password" required />
      <button type="submit">Sign In</button>
    </form>
  </div>
</body>
</html>`, url.QueryEscape(returnTo))
		c.Set("Content-Type", "text/html; charset=utf-8")
		return c.SendString(html)
	}

	username := strings.TrimSpace(c.FormValue("username"))
	password := c.FormValue("password")
	if username == "" || password == "" {
		return c.Status(fiber.StatusBadRequest).SendString("Username and password required")
	}

	if a.store == nil {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Local auth store unconfigured")
	}

	user, hash, err := a.store.GetUser(c.UserContext(), username)
	if err != nil || user == nil {
		slog.Warn("local auth: user not found", "username", username)
		return c.Status(fiber.StatusUnauthorized).SendString("Invalid credentials")
	}

	match, err := VerifyPassword(password, hash)
	if err != nil || !match {
		slog.Warn("local auth: invalid password", "username", username)
		return c.Status(fiber.StatusUnauthorized).SendString("Invalid credentials")
	}

	data, err := json.Marshal(sessionData{
		Username: user.Username,
		Email:    user.Email,
		Exp:      time.Now().Add(sessionTTL).Unix(),
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Session error")
	}

	c.Cookie(&fiber.Cookie{
		Name:     sessionCookie,
		Value:    a.sign(data),
		HTTPOnly: true,
		Secure:   a.isSecure,
		SameSite: "Lax",
		Path:     "/",
		Expires:  time.Now().Add(sessionTTL),
	})

	slog.Info("local auth: user signed in", "username", user.Username)
	return c.Redirect(returnTo, fiber.StatusFound)
}

func (a *Authenticator) Callback(c *fiber.Ctx) error {
	return c.Redirect("/", fiber.StatusFound)
}

func (a *Authenticator) Logout(c *fiber.Ctx) error {
	c.Cookie(&fiber.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		Expires:  time.Now().Add(-24 * time.Hour),
		MaxAge:   -1,
		Secure:   a.isSecure,
		HTTPOnly: true,
		SameSite: "Lax",
	})
	return c.Redirect("/", fiber.StatusFound)
}
