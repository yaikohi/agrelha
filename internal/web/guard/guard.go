package guard

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"agrelha/internal/app/authz"
	"agrelha/internal/domain"
	"agrelha/internal/ports"
	"agrelha/internal/web/shared"

	"github.com/gofiber/fiber/v2"
)

func wantsStream(c *fiber.Ctx) bool {
	return c.Get("Datastar-Request") == "true" || strings.Contains(c.Get("Accept"), "text/event-stream")
}

func wantsJSON(c *fiber.Ctx) bool {
	return strings.Contains(c.Get("Accept"), "application/json")
}

func streamRedirect(c *fiber.Ctx, target string) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	return c.SendString(fmt.Sprintf(
		"event: datastar-patch-elements\ndata: mode append\ndata: selector body\ndata: elements <script>window.location.href = %q</script>\n\n",
		target))
}

func streamToast(c *fiber.Ctx, msg string) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	return c.SendString(fmt.Sprintf(
		"event: datastar-patch-signals\ndata: signals {\"toastKind\":\"err\",\"toastMsg\":%q}\n\n", msg))
}

func Unauthenticated(c *fiber.Ctx) error {
	target := "/auth/login"
	if rt := c.OriginalURL(); rt != "" && rt != "/" {
		target += "?returnTo=" + url.QueryEscape(rt)
	}
	if wantsStream(c) {
		return streamRedirect(c, target)
	}
	if wantsJSON(c) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "not signed in"})
	}
	return c.Redirect(target, fiber.StatusFound)
}

func Forbidden(c *fiber.Ctx, msg string) error {
	if msg == "" {
		msg = "You do not have access to that."
	}
	if wantsStream(c) {
		return streamToast(c, msg)
	}
	if wantsJSON(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
	}
	shared.SetFlash(c, "err", msg)
	return c.Redirect("/", fiber.StatusSeeOther)
}

func Optional(auth ports.Auth, svc *authz.Service) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if auth == nil {
			return c.Next()
		}
		id, ok := auth.Identify(c)
		if !ok {
			return c.Next()
		}
		p := domain.NewPrincipal(id)
		c.Locals(shared.PrincipalKey, p)
		actor := id.Email
		if actor == "" {
			actor = id.Subject
		}
		c.Locals("actor", actor)
		return c.Next()
	}
}

func Required() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !shared.PrincipalOf(c).SignedIn() {
			return Unauthenticated(c)
		}
		return c.Next()
	}
}

func Admin() fiber.Handler {
	return func(c *fiber.Ctx) error {
		p := shared.PrincipalOf(c)
		if !p.SignedIn() {
			return Unauthenticated(c)
		}
		if !p.Admin {
			return Forbidden(c, "That area is for administrators only.")
		}
		return c.Next()
	}
}

func Instance(game domain.GameID) fiber.Handler {
	return func(c *fiber.Ctx) error {
		p := shared.PrincipalOf(c)
		if !p.SignedIn() {
			return Unauthenticated(c)
		}
		num, err := strconv.Atoi(c.Params("num"))
		if err != nil || num <= 0 {
			return Forbidden(c, "Unknown instance.")
		}
		if c.Method() == fiber.MethodDelete && isBareInstancePath(c, num) {
			if !p.Admin {
				return Forbidden(c, "Only administrators can delete a world.")
			}
			return c.Next()
		}
		if !p.CanOperate(game, num) {
			return Forbidden(c, "You do not have access to that world.")
		}
		return c.Next()
	}
}

func isBareInstancePath(c *fiber.Ctx, num int) bool {
	p := strings.TrimSuffix(c.Path(), "/")
	return strings.HasSuffix(p, "/instances/"+strconv.Itoa(num))
}

func AdminExact(paths ...string) fiber.Handler {
	exact := make(map[string]bool, len(paths))
	for _, p := range paths {
		exact[p] = true
	}
	return func(c *fiber.Ctx) error {
		if !exact[strings.TrimSuffix(c.Path(), "/")] {
			return c.Next()
		}
		p := shared.PrincipalOf(c)
		if !p.SignedIn() {
			return Unauthenticated(c)
		}
		if !p.Admin {
			return Forbidden(c, "That area is for administrators only.")
		}
		return c.Next()
	}
}

type Mount struct {
	Prefix string
	Game   domain.GameID
	Admin  bool
	Exact  bool
}

func Mounts() []Mount {
	return []Mount{
		{Prefix: "/api/valheim/instances/:num", Game: domain.GameValheim},
		{Prefix: "/api/valheim/:num<int>", Game: domain.GameValheim},
		{Prefix: "/valheim/:num<int>", Game: domain.GameValheim},
		{Prefix: "/api/minecraft/instances/:num", Game: domain.GameMinecraft},
		{Prefix: "/api/minecraft/:num<int>", Game: domain.GameMinecraft},
		{Prefix: "/minecraft/:num<int>", Game: domain.GameMinecraft},
		{Prefix: "/minecraft/provisioning/:num", Game: domain.GameMinecraft},
		{Prefix: "/api/minecraft/provisioning/:num", Game: domain.GameMinecraft},

		{Prefix: "/admins", Admin: true},
		{Prefix: "/history", Admin: true},
		{Prefix: "/accounts", Admin: true},
		{Prefix: "/requests", Admin: true},
		{Prefix: "/minecraft/access", Admin: true},
		{Prefix: "/api/minecraft/access", Admin: true},
		{Prefix: "/minecraft/configs", Admin: true},
		{Prefix: "/server", Admin: true},
		{Prefix: "/minecraft/server", Admin: true},
		{Prefix: "/sse/logs", Admin: true},
	}
}

func Apply(router fiber.Router) {
	var exact []string
	for _, m := range Mounts() {
		switch {
		case m.Exact:
			exact = append(exact, m.Prefix)
		case m.Admin:
			router.Use(m.Prefix, Admin())
		default:
			router.Use(m.Prefix, Instance(m.Game))
		}
	}
	if len(exact) > 0 {
		router.Use(AdminExact(exact...))
	}
}
