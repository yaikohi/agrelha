package access

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	mcaccess "agrelha/internal/app/access"

	"github.com/gofiber/fiber/v2"
)

func TestMCAccess_UsernameValidation(t *testing.T) {
	state := newMemStateStore()
	console := &mockAccessConsole{
		responses: map[string]string{
			"/op Steve":               "Made Steve a server operator",
			"/deop Steve":             "Made Steve no longer a server operator",
			"/whitelist add Steve":    "Added Steve to whitelist",
			"/whitelist remove Steve": "Removed Steve from whitelist",
			"/op Alex_123":            "Made Alex_123 a server operator",
			"/deop Alex_123":          "Made Alex_123 no longer a server operator",
			"/whitelist add Alex_123": "Added Alex_123 to whitelist",
			"/whitelist remove Alex_123": "Removed Alex_123 from whitelist",
			"/op Notch":               "Made Notch a server operator",
			"/deop Notch":             "Made Notch no longer a server operator",
			"/whitelist add Notch":    "Added Notch to whitelist",
			"/whitelist remove Notch": "Removed Notch from whitelist",
			"/whitelist reload":       "Reloaded whitelist",
		},
	}
	mcAcc := mcaccess.NewAccessManager(state, "access.yaml", console)
	h := New(Config{MCAccess: mcAcc})
	app := fiber.New()
	h.Register(app)

	endpoints := []struct {
		name string
		path string
	}{
		{name: "GrantOp", path: "/api/minecraft/access/op"},
		{name: "RevokeOp", path: "/api/minecraft/access/deop"},
		{name: "AddWhitelist", path: "/api/minecraft/access/whitelist/add"},
		{name: "RemoveWhitelist", path: "/api/minecraft/access/whitelist/remove"},
	}

	invalidInputs := []struct {
		name     string
		username string
	}{
		{name: "newline injection", username: "steve\n/op malicious"},
		{name: "semicolon / commands", username: "player;kick all"},
		{name: "too long (>16 chars)", username: "player_with_a_name_that_is_far_too_long"},
		{name: "spaces", username: "user name"},
		{name: "symbols", username: "steve!"},
		{name: "empty", username: ""},
	}

	for _, ep := range endpoints {
		for _, inv := range invalidInputs {
			t.Run(ep.name+"_JSON_Invalid_"+inv.name, func(t *testing.T) {
				form := url.Values{}
				form.Set("username", inv.username)
				req := httptest.NewRequest(http.MethodPost, ep.path, strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Accept", "application/json")

				resp, err := app.Test(req)
				if err != nil {
					t.Fatalf("app.Test error: %v", err)
				}
				if resp.StatusCode != fiber.StatusBadRequest {
					t.Fatalf("expected status 400 for username %q, got %d", inv.username, resp.StatusCode)
				}
			})

			t.Run(ep.name+"_HTML_Invalid_"+inv.name, func(t *testing.T) {
				form := url.Values{}
				form.Set("username", inv.username)
				req := httptest.NewRequest(http.MethodPost, ep.path, strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Accept", "text/html")

				resp, err := app.Test(req)
				if err != nil {
					t.Fatalf("app.Test error: %v", err)
				}
				if resp.StatusCode != fiber.StatusSeeOther {
					t.Fatalf("expected status 303 for username %q, got %d", inv.username, resp.StatusCode)
				}
			})
		}
	}

	validNames := []string{"Steve", "Alex_123", "Notch"}
	for _, ep := range endpoints {
		for _, valid := range validNames {
			t.Run(ep.name+"_JSON_Valid_"+valid, func(t *testing.T) {
				form := url.Values{}
				form.Set("username", valid)
				req := httptest.NewRequest(http.MethodPost, ep.path, strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Accept", "application/json")

				resp, err := app.Test(req)
				if err != nil {
					t.Fatalf("app.Test error: %v", err)
				}
				if resp.StatusCode != fiber.StatusOK {
					t.Fatalf("expected status 200 for valid username %q, got %d", valid, resp.StatusCode)
				}
			})
		}
	}
}
