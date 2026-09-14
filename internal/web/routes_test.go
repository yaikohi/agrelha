package web_test

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/web"
	"agrelha/internal/web/handlers/access"
	backupshttp "agrelha/internal/web/handlers/backups"
	consolehttp "agrelha/internal/web/handlers/console"
	contenthttp "agrelha/internal/web/handlers/content"
	dashboardhttp "agrelha/internal/web/handlers/dashboard"
	minecrafthttp "agrelha/internal/web/handlers/minecraft"
	valheimhttp "agrelha/internal/web/handlers/valheim"
	wizardhttp "agrelha/internal/web/handlers/wizard"
)

type mockRoutesAuth struct{}

func (m *mockRoutesAuth) IsAuthenticated(c *fiber.Ctx) bool { return true }
func (m *mockRoutesAuth) Middleware() fiber.Handler         { return func(c *fiber.Ctx) error { return c.Next() } }
func (m *mockRoutesAuth) Login(c *fiber.Ctx) error          { return c.SendString("login") }
func (m *mockRoutesAuth) Callback(c *fiber.Ctx) error       { return c.SendString("callback") }
func (m *mockRoutesAuth) Logout(c *fiber.Ctx) error         { return c.SendString("logout") }

func TestRoutes_HealthzAndMetrics(t *testing.T) {
	app := web.New(web.ServerConfig{})

	req := httptest.NewRequest(fiber.MethodGet, "/healthz", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("GET /healthz status = %d, want 200", resp.StatusCode)
	}

	reqMetrics := httptest.NewRequest(fiber.MethodGet, "/metrics", nil)
	respMetrics, err := app.Test(reqMetrics)
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	if respMetrics.StatusCode != fiber.StatusOK {
		t.Errorf("GET /metrics status = %d, want 200", respMetrics.StatusCode)
	}

	reqAsset := httptest.NewRequest(fiber.MethodGet, "/assets/css/output.css", nil)
	respAsset, err := app.Test(reqAsset)
	if err != nil {
		t.Fatalf("GET /assets/css/output.css: %v", err)
	}
	if respAsset.StatusCode != fiber.StatusOK {
		t.Errorf("GET /assets/css/output.css status = %d, want 200", respAsset.StatusCode)
	}
}

func TestRoutes_AllHandlersMounted(t *testing.T) {
	auth := &mockRoutesAuth{}
	app := web.New(web.ServerConfig{
		Auth:      auth,
		Access:    access.New(access.Config{}),
		Backups:   backupshttp.New(backupshttp.Config{}),
		Console:   consolehttp.New(consolehttp.Config{}),
		Content:   contenthttp.New(contenthttp.Config{}),
		Dashboard: dashboardhttp.New(dashboardhttp.Config{}),
		Minecraft: minecrafthttp.New(minecrafthttp.Config{}),
		Valheim:   valheimhttp.New(valheimhttp.Config{}),
		Wizard:    wizardhttp.New(wizardhttp.Config{}),
	})

	// 1. Dashboard root
	reqRoot := httptest.NewRequest(fiber.MethodGet, "/", nil)
	respRoot, err := app.Test(reqRoot)
	if err != nil || respRoot.StatusCode != fiber.StatusOK {
		t.Errorf("GET / status = %d, err = %v", respRoot.StatusCode, err)
	}

	// 2. Auth routes
	reqLoginNoQuery := httptest.NewRequest(fiber.MethodGet, "/login", nil)
	respLoginNoQuery, _ := app.Test(reqLoginNoQuery)
	if respLoginNoQuery.StatusCode != fiber.StatusFound || respLoginNoQuery.Header.Get("Location") != "/auth/login" {
		t.Errorf("unexpected redirect for /login: %d, %s", respLoginNoQuery.StatusCode, respLoginNoQuery.Header.Get("Location"))
	}

	reqLoginQuery := httptest.NewRequest(fiber.MethodGet, "/login?next=/dash", nil)
	respLoginQuery, _ := app.Test(reqLoginQuery)
	if respLoginQuery.StatusCode != fiber.StatusFound || respLoginQuery.Header.Get("Location") != "/auth/login?next=/dash" {
		t.Errorf("unexpected redirect for /login with query: %d, %s", respLoginQuery.StatusCode, respLoginQuery.Header.Get("Location"))
	}

	for _, endpoint := range []struct {
		method string
		path   string
		body   string
	}{
		{fiber.MethodGet, "/auth/login", "login"},
		{fiber.MethodPost, "/auth/login", "login"},
		{fiber.MethodGet, "/auth/callback", "callback"},
		{fiber.MethodGet, "/auth/logout", "logout"},
	} {
		req := httptest.NewRequest(endpoint.method, endpoint.path, nil)
		resp, err := app.Test(req)
		if err != nil || resp.StatusCode != fiber.StatusOK {
			t.Errorf("%s %s failed: status = %d, err = %v", endpoint.method, endpoint.path, resp.StatusCode, err)
		}
	}
}
