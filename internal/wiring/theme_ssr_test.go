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

func TestThemeIsServerRenderedFromTheCookie(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	app := wiring.BuildServer(context.Background(), &config.Config{}, wiring.Deps{
		Store: st,
		Auth:  auth.NewDev(auth.Config{}, auth.WithSessions(st)),
	})

	get := func(theme string) string {
		t.Helper()
		req := httptest.NewRequest(fiber.MethodGet, "/", nil)
		if theme != "" {
			req.AddCookie(&http.Cookie{Name: "agrelha_theme", Value: theme})
		}
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}

	for _, tc := range []struct{ cookie, want string }{
		{"", `data-theme="dark"`},
		{"dark", `data-theme="dark"`},
		{"light", `data-theme="light"`},
		{"garbage", `data-theme="dark"`},
	} {
		body := get(tc.cookie)
		if !strings.Contains(body, tc.want) {
			t.Errorf("cookie %q: expected %s in the served HTML", tc.cookie, tc.want)
		}
		head := body
		if before, _, ok := strings.Cut(body, `<link rel="stylesheet"`); ok {
			head = before
		} else {
			t.Fatal("no stylesheet link found; the no-flash guarantee cannot be checked")
		}
		if !strings.Contains(head, "agrelha-theme") {
			t.Errorf("cookie %q: the theme bootstrap script must come BEFORE the stylesheet or the page paints the wrong palette first", tc.cookie)
		}
		if !strings.Contains(head, tc.want) {
			t.Errorf("cookie %q: data-theme must be in the <html> tag, ahead of the stylesheet", tc.cookie)
		}
	}
}

func TestThemeToggleIsPresentAndWired(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	app := wiring.BuildServer(context.Background(), &config.Config{}, wiring.Deps{
		Store: st,
		Auth:  auth.NewDev(auth.Config{}, auth.WithSessions(st)),
	})

	req := httptest.NewRequest(fiber.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "agrelha_theme", Value: "light"})
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	body := string(b)

	for _, want := range []string{
		"window.agrelhaTheme",
		"theme:&#39;light&#39;",
		"agrelhaTheme($theme == 'light' ? 'dark' : 'light')",
		"Switch to dark theme",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("served page is missing %q; the toggle cannot work without it", want)
		}
	}

	if !strings.Contains(resp.Header.Get("Vary"), "Cookie") {
		t.Errorf("Vary must include Cookie: the HTML now differs by the theme cookie, got %q", resp.Header.Get("Vary"))
	}
}
