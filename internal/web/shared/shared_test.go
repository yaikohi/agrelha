package shared

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/gofiber/fiber/v2"

	"agrelha/internal/ports"
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
	for p := range strings.SplitSeq(cookie, ";") {
		p = strings.TrimSpace(p)
		if after, ok := strings.CutPrefix(p, flashCookie+"="); ok {
			cookieVal = after
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

type mockAuth struct {
	authenticated bool
}

func (m *mockAuth) Middleware() fiber.Handler         { return func(c *fiber.Ctx) error { return c.Next() } }
func (m *mockAuth) IsAuthenticated(c *fiber.Ctx) bool { return m.authenticated }
func (m *mockAuth) Login(c *fiber.Ctx) error          { return nil }
func (m *mockAuth) Callback(c *fiber.Ctx) error       { return nil }
func (m *mockAuth) Logout(c *fiber.Ctx) error         { return nil }

func TestSharedRemainingEdges(t *testing.T) {
	app := fiber.New()

	// 1. TakeFlash with corrupt base64
	app.Get("/take-corrupt", func(c *fiber.Ctx) error {
		k, m := TakeFlash(c)
		return c.SendString(k + ":" + m)
	})
	reqCorrupt := httptest.NewRequest(http.MethodGet, "/take-corrupt", nil)
	reqCorrupt.AddCookie(&http.Cookie{Name: flashCookie, Value: "!!!not-valid-base64!!!"})
	respCorrupt, _ := app.Test(reqCorrupt)
	body := make([]byte, 50)
	n, _ := respCorrupt.Body.Read(body)
	if string(body[:n]) != ":" {
		t.Errorf("expected empty flash on corrupt cookie, got %q", string(body[:n]))
	}

	// 2. IsAdmin with Auth implementation
	app.Get("/is-admin-auth", func(c *fiber.Ctx) error {
		var auth ports.Auth = &mockAuth{authenticated: c.Query("admin") == "1"}
		if IsAdmin(auth, c) {
			return c.SendString("true")
		}
		return c.SendString("false")
	})
	reqAuthTrue := httptest.NewRequest(http.MethodGet, "/is-admin-auth?admin=1", nil)
	respAuthTrue, _ := app.Test(reqAuthTrue)
	n, _ = respAuthTrue.Body.Read(body)
	if string(body[:n]) != "true" {
		t.Errorf("expected true, got %q", string(body[:n]))
	}
	reqAuthFalse := httptest.NewRequest(http.MethodGet, "/is-admin-auth?admin=0", nil)
	respAuthFalse, _ := app.Test(reqAuthFalse)
	n, _ = respAuthFalse.Body.Read(body)
	if string(body[:n]) != "false" {
		t.Errorf("expected false, got %q", string(body[:n]))
	}

	// 3. Render
	app.Get("/render", func(c *fiber.Ctx) error {
		comp := templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
			_, err := w.Write([]byte("<h1>Hello Templ</h1>"))
			return err
		})
		return Render(c, comp)
	})
	reqRender := httptest.NewRequest(http.MethodGet, "/render", nil)
	respRender, _ := app.Test(reqRender)
	n, _ = respRender.Body.Read(body)
	if !strings.Contains(string(body[:n]), "Hello Templ") {
		t.Errorf("expected rendered html, got %q", string(body[:n]))
	}
	if ct := respRender.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content type, got %s", ct)
	}

	// 4. SSEToast
	app.Get("/toast", func(c *fiber.Ctx) error {
		return SSEToast(c, "info", "Server started", map[string]any{"custom": 123})
	})
	reqToast := httptest.NewRequest(http.MethodGet, "/toast", nil)
	respToast, _ := app.Test(reqToast)
	bufToast := make([]byte, 200)
	n, _ = respToast.Body.Read(bufToast)
	toastBody := string(bufToast[:n])
	if !strings.Contains(toastBody, "event: datastar-patch-signals") || !strings.Contains(toastBody, "Server started") {
		t.Errorf("unexpected toast body: %s", toastBody)
	}
	if ct := respToast.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %s", ct)
	}

	// 5. RequestLogger branches
	logApp := fiber.New()
	logApp.Use(RequestLogger())
	logApp.Get("/healthz", func(c *fiber.Ctx) error { return c.SendString("ok") })
	logApp.Get("/metrics", func(c *fiber.Ctx) error { return c.SendString("metrics") })
	logApp.Get("/assets/style.css", func(c *fiber.Ctx) error { return c.SendString("css") })
	logApp.Get("/redirect", func(c *fiber.Ctx) error {
		c.Locals("actor", "bob")
		return c.Redirect("/destination")
	})
	logApp.Get("/err400", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusBadRequest).SendString("bad")
	})
	logApp.Get("/err500", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusInternalServerError).SendString("server err")
	})
	logApp.Get("/datastar", func(c *fiber.Ctx) error {
		return c.SendString("ds")
	})

	for _, path := range []string{"/healthz", "/metrics", "/assets/style.css", "/redirect", "/err400", "/err500"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		_, _ = logApp.Test(req)
	}
	reqDS := httptest.NewRequest(http.MethodGet, "/datastar", nil)
	reqDS.Header.Set("Datastar-Request", "true")
	_, _ = logApp.Test(reqDS)

	// 6. RequestID when not set
	emptyCtxApp := fiber.New()
	emptyCtxApp.Get("/empty-rid", func(c *fiber.Ctx) error {
		return c.SendString(RequestID(c))
	})
	respEmptyRID, _ := emptyCtxApp.Test(httptest.NewRequest(http.MethodGet, "/empty-rid", nil))
	n, _ = respEmptyRID.Body.Read(body)
	if string(body[:n]) != "-" {
		t.Errorf("expected '-', got %q", string(body[:n]))
	}
}
