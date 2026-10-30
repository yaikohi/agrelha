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

	auth "agrelha/internal/infra/auth/oidc"
	"agrelha/internal/infra/store"
	"agrelha/internal/platform/config"
	"agrelha/internal/wiring"
)

func TestSignOutActuallyChangesTheRenderedState(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	devAuth := auth.NewDev(auth.Config{}, auth.WithSessions(st))
	app := wiring.BuildServer(context.Background(), &config.Config{}, wiring.Deps{
		Store: st,
		Auth:  devAuth,
	})

	get := func(path, session string) (*http.Response, string) {
		t.Helper()
		req := httptest.NewRequest(fiber.MethodGet, path, nil)
		if session != "" {
			req.AddCookie(&http.Cookie{Name: "agrelha_session", Value: session})
		}
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		return resp, string(body)
	}

	loginResp, _ := get("/auth/login", "")
	var session string
	for _, c := range loginResp.Header.Values("Set-Cookie") {
		if strings.HasPrefix(c, "agrelha_session=") {
			session = strings.TrimPrefix(strings.Split(c, ";")[0], "agrelha_session=")
		}
	}
	if session == "" {
		t.Fatal("dev login set no session cookie")
	}

	_, signedIn := get("/", session)
	if !strings.Contains(signedIn, "sign out") {
		t.Fatal("precondition failed: the signed-in dashboard has no sign-out link")
	}

	logoutResp, _ := get("/auth/logout", session)
	if logoutResp.StatusCode != fiber.StatusFound {
		t.Fatalf("logout status = %d, want 302", logoutResp.StatusCode)
	}

	_, after := get("/", session)
	if strings.Contains(after, "sign out") {
		t.Error("after signing out, replaying the old session cookie still renders the signed-in page: the session was not invalidated server-side, so a browser that keeps the cookie stays logged in")
	}
	if !strings.Contains(after, "Sign in") {
		t.Error("after signing out, the page does not offer a way to sign back in")
	}
}
