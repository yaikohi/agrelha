package guard_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/domain"
	"agrelha/internal/web/guard"
	"agrelha/internal/web/shared"
)

func appWith(p domain.Principal, mount fiber.Handler) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		if p.SignedIn() {
			c.Locals(shared.PrincipalKey, p)
		}
		return c.Next()
	})
	app.Use("/valheim/:num<int>", mount)
	app.Get("/valheim/:num<int>/overview", func(c *fiber.Ctx) error {
		return c.SendString("reached")
	})
	return app
}

func grantee(nums ...int) domain.Principal {
	id := domain.Identity{Subject: "friend", Email: "friend@example.com"}
	for _, n := range nums {
		id.Roles = append(id.Roles, domain.InstanceRole(domain.GameValheim, n))
	}
	return domain.NewPrincipal(id)
}

func TestDenyShapes(t *testing.T) {
	app := appWith(grantee(2), guard.Instance(domain.GameValheim))

	t.Run("page redirects with a flash", func(t *testing.T) {
		req := httptest.NewRequest(fiber.MethodGet, "/valheim/1/overview", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusSeeOther {
			t.Errorf("status = %d, want 303", resp.StatusCode)
		}
		if loc := resp.Header.Get("Location"); loc != "/" {
			t.Errorf("Location = %q, want /", loc)
		}
	})

	t.Run("datastar gets 200 with a toast", func(t *testing.T) {
		req := httptest.NewRequest(fiber.MethodGet, "/valheim/1/overview", nil)
		req.Header.Set("Datastar-Request", "true")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		// Datastar discards a non-2xx as a transport error, so a denial must
		// arrive as 200 carrying an error toast or the user sees nothing.
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("status = %d, want 200 so the patch is not discarded", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "datastar-patch-signals") {
			t.Errorf("expected a datastar signal patch, got %q", string(body))
		}
	})

	t.Run("json gets 403", func(t *testing.T) {
		req := httptest.NewRequest(fiber.MethodGet, "/valheim/1/overview", nil)
		req.Header.Set("Accept", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusForbidden {
			t.Errorf("status = %d, want 403", resp.StatusCode)
		}
	})
}

func TestGrantAllowsOnlyTheGrantedInstance(t *testing.T) {
	app := appWith(grantee(2), guard.Instance(domain.GameValheim))

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/valheim/2/overview", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("granted instance: status = %d, want 200", resp.StatusCode)
	}

	resp2, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/valheim/3/overview", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp2.StatusCode == fiber.StatusOK {
		t.Error("ungranted instance must not be reachable")
	}
}

func TestAnonymousIsSentToLogin(t *testing.T) {
	app := appWith(domain.Principal{}, guard.Instance(domain.GameValheim))

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/valheim/2/overview", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusFound {
		t.Errorf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "/auth/login") {
		t.Errorf("Location = %q, want /auth/login...", loc)
	}
}

func TestAdminReachesEveryInstance(t *testing.T) {
	admin := domain.NewPrincipal(domain.Identity{
		Subject: "boss", Roles: []domain.Role{domain.RoleAdmin},
	})
	app := appWith(admin, guard.Instance(domain.GameValheim))

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/valheim/9/overview", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("admin status = %d, want 200", resp.StatusCode)
	}
}

func TestNonAdminCannotDeleteOwnInstance(t *testing.T) {
	app := fiber.New()
	p := grantee(2)
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(shared.PrincipalKey, p)
		return c.Next()
	})
	app.Use("/api/valheim/instances/:num", guard.Instance(domain.GameValheim))
	app.Delete("/api/valheim/instances/:num", func(c *fiber.Ctx) error {
		return c.SendString("deleted")
	})
	app.Post("/api/valheim/instances/:num/start", func(c *fiber.Ctx) error {
		return c.SendString("started")
	})

	del := httptest.NewRequest(fiber.MethodDelete, "/api/valheim/instances/2", nil)
	del.Header.Set("Accept", "application/json")
	resp, err := app.Test(del)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("delete status = %d, want 403 even on a granted instance", resp.StatusCode)
	}

	start, err := app.Test(httptest.NewRequest(fiber.MethodPost, "/api/valheim/instances/2/start", nil))
	if err != nil {
		t.Fatal(err)
	}
	if start.StatusCode != fiber.StatusOK {
		t.Errorf("start status = %d, want 200 on a granted instance", start.StatusCode)
	}
}

func TestNonNumericInstanceIsRefused(t *testing.T) {
	app := fiber.New()
	p := grantee(2)
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(shared.PrincipalKey, p)
		return c.Next()
	})
	app.Use("/api/valheim/instances/:num", guard.Instance(domain.GameValheim))
	app.Post("/api/valheim/instances/:num/start", func(c *fiber.Ctx) error {
		return c.SendString("started")
	})

	req := httptest.NewRequest(fiber.MethodPost, "/api/valheim/instances/abc/start", nil)
	req.Header.Set("Accept", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == fiber.StatusOK {
		t.Error("a non-numeric instance id must not reach the handler")
	}
}
