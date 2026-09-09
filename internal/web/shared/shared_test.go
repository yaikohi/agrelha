package shared

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

func TestFlashSetAndTake(t *testing.T) {
	app := fiber.New()
	app.Get("/set", func(c *fiber.Ctx) error {
		SetFlash(c, "success", "All systems operational")
		return c.SendString("ok")
	})
	app.Get("/take", func(c *fiber.Ctx) error {
		k, m := TakeFlash(c)
		return c.SendString(k + ":" + m)
	})

	// 1. Set flash
	reqSet := httptest.NewRequest(http.MethodGet, "/set", nil)
	respSet, err := app.Test(reqSet)
	if err != nil {
		t.Fatal(err)
	}
	cookie := respSet.Header.Get("Set-Cookie")
	if !strings.Contains(cookie, flashCookie) {
		t.Fatalf("expected flash cookie, got %s", cookie)
	}

	var cookieVal string
	for _, p := range strings.Split(cookie, ";") {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, flashCookie+"=") {
			cookieVal = strings.TrimPrefix(p, flashCookie+"=")
			break
		}
	}

	// 2. Take flash
	reqTake := httptest.NewRequest(http.MethodGet, "/take", nil)
	reqTake.AddCookie(&http.Cookie{Name: flashCookie, Value: cookieVal})
	respTake, err := app.Test(reqTake)
	if err != nil {
		t.Fatal(err)
	}
	body := make([]byte, 100)
	n, _ := respTake.Body.Read(body)
	if string(body[:n]) != "success:All systems operational" {
		t.Errorf("got %q, want success:All systems operational", string(body[:n]))
	}

	// 3. Take empty flash
	reqEmpty := httptest.NewRequest(http.MethodGet, "/take", nil)
	respEmpty, _ := app.Test(reqEmpty)
	n, _ = respEmpty.Body.Read(body)
	if string(body[:n]) != ":" {
		t.Errorf("got %q, want empty ':'", string(body[:n]))
	}
}

func TestFormatHelpers(t *testing.T) {
	if got := HumanAgo(time.Time{}); got != "—" {
		t.Errorf("HumanAgo(zero) = %q, want '—'", got)
	}
	if got := HumanAgo(time.Now().Add(-10 * time.Second)); got != "just now" {
		t.Errorf("HumanAgo(10s ago) = %q, want 'just now'", got)
	}
	if got := HumanAgo(time.Now().Add(-2 * time.Hour)); got != "2h 0m ago" {
		t.Errorf("HumanAgo(2h ago) = %q, want '2h 0m ago'", got)
	}

	if got := HumanSize(500); got != "500 B" {
		t.Errorf("HumanSize(500) = %q, want '500 B'", got)
	}
	if got := HumanSize(1024 * 1024 * 10); got != "10.0 MB" {
		t.Errorf("HumanSize(10MB) = %q, want '10.0 MB'", got)
	}

	if got := HumanDuration(25 * time.Hour); got != "1d 1h" {
		t.Errorf("HumanDuration(25h) = %q, want '1d 1h'", got)
	}
	if got := HumanDuration(45 * time.Minute); got != "45m" {
		t.Errorf("HumanDuration(45m) = %q, want '45m'", got)
	}
}

func TestActorAndIsAdmin(t *testing.T) {
	app := fiber.New()
	app.Get("/actor", func(c *fiber.Ctx) error {
		return c.SendString(Actor(c))
	})
	app.Get("/actor-set", func(c *fiber.Ctx) error {
		c.Locals("actor", "admin@example.com")
		return c.SendString(Actor(c))
	})

	req1 := httptest.NewRequest(http.MethodGet, "/actor", nil)
	resp1, _ := app.Test(req1)
	buf := make([]byte, 100)
	n, _ := resp1.Body.Read(buf)
	if string(buf[:n]) != "-" {
		t.Errorf("got actor %q, want '-'", string(buf[:n]))
	}

	req2 := httptest.NewRequest(http.MethodGet, "/actor-set", nil)
	resp2, _ := app.Test(req2)
	n, _ = resp2.Body.Read(buf)
	if string(buf[:n]) != "admin@example.com" {
		t.Errorf("got actor %q, want admin@example.com", string(buf[:n]))
	}

	app.Get("/is-admin", func(c *fiber.Ctx) error {
		if IsAdmin(nil, c) {
			return c.SendString("true")
		}
		return c.SendString("false")
	})

	reqAdmin := httptest.NewRequest(http.MethodGet, "/is-admin", nil)
	respAdmin, _ := app.Test(reqAdmin)
	n, _ = respAdmin.Body.Read(buf)
	if string(buf[:n]) != "true" {
		t.Errorf("IsAdmin(nil) should be true, got %q", string(buf[:n]))
	}
}

func TestRequestLoggerAndID(t *testing.T) {
	app := fiber.New()
	app.Use(RequestLogger())
	app.Get("/hello", func(c *fiber.Ctx) error {
		return c.SendString(RequestID(c))
	})

	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 100)
	n, _ := resp.Body.Read(buf)
	id := string(buf[:n])
	if id == "" || id == "-" {
		t.Errorf("expected generated request ID, got %q", id)
	}
}
