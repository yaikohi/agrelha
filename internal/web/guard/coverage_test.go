package guard_test

import (
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/web"
	"agrelha/internal/web/guard"
	"agrelha/internal/web/handlers/access"
	backupshttp "agrelha/internal/web/handlers/backups"
	consolehttp "agrelha/internal/web/handlers/console"
	contenthttp "agrelha/internal/web/handlers/content"
	dashboardhttp "agrelha/internal/web/handlers/dashboard"
	gmodhttp "agrelha/internal/web/handlers/gmod"
	grantshttp "agrelha/internal/web/handlers/grants"
	minecrafthttp "agrelha/internal/web/handlers/minecraft"
	valheimhttp "agrelha/internal/web/handlers/valheim"
	wizardhttp "agrelha/internal/web/handlers/wizard"
)

var publicRoutes = map[string]bool{
	"/":                                    true,
	"/sse":                                 true,
	"/healthz":                             true,
	"/metrics":                             true,
	"/img":                                 true,
	"/login":                               true,
	"/auth/login":                          true,
	"/auth/callback":                       true,
	"/auth/logout":                         true,
	"/assets":                              true,
	"/assets/+":                            true,
	"/mods/export":                         true,
	"/valheim/mods/export":                 true,
	"/api/valheim/:num<int>/mods/export":   true,
	"/api/minecraft/:num<int>/mods/export": true,
}

// anyAccount lists protected routes that every signed-in Account may reach,
// regardless of which Instances they hold a Role for. Adding a route here is a
// deliberate decision that it leaks nothing an Account should not see.
var anyAccount = map[string]bool{
	"/valheim":               true,
	"/minecraft":             true,
	"/valheim/console":       true,
	"/mods/:namespace/:name": true,
	"/valheim/mods":          true,
	"/valheim/configs":       true,
	"/valheim/access":        true,
	"/gmod":                  true,
	"/gmod/access":           true,
	"/gmod/create":           true,
	"/mods":                  true,
	"/configs":               true,
	"/minecraft/mods":        true,

	"/valheim/create":          true,
	"/minecraft/create":        true,
	"/api/minecraft/instances": true,
}

// anyAccountPrefixes are whole surfaces every signed-in Account may use. The
// creation wizards live here because any Account may create a world: whether
// that produces a world directly or a pending request is decided by
// app/requests in the handler, not by the guard.
var anyAccountPrefixes = []string{
	"/api/valheim/wizard",
	"/api/minecraft/wizard",
	"/api/gmod/wizard",
}

func buildApp() *fiber.App {
	return web.New(web.ServerConfig{
		Access:    access.New(access.Config{}),
		Backups:   backupshttp.New(backupshttp.Config{}),
		Console:   consolehttp.New(consolehttp.Config{}),
		Content:   contenthttp.New(contenthttp.Config{}),
		Dashboard: dashboardhttp.New(dashboardhttp.Config{}),
		GMod:      gmodhttp.New(gmodhttp.Config{}),
		Grants:    grantshttp.New(grantshttp.Config{}),
		Minecraft: minecrafthttp.New(minecrafthttp.Config{}),
		Valheim:   valheimhttp.New(valheimhttp.Config{}),
		Wizard:    wizardhttp.New(wizardhttp.Config{}),
	})
}

// normalise makes ":num" and ":num<int>" compare equal, so the ledger matches
// how a handler actually registered its route rather than how it is spelled.
func normalise(p string) []string {
	var out []string
	for seg := range strings.SplitSeq(strings.Trim(p, "/"), "/") {
		if strings.HasPrefix(seg, ":") {
			if i := strings.Index(seg, "<"); i >= 0 {
				seg = seg[:i]
			}
		}
		out = append(out, seg)
	}
	return out
}

func prefixOf(mount, path string) bool {
	m, p := normalise(mount), normalise(path)
	if len(m) > len(p) {
		return false
	}
	for i := range m {
		if m[i] != p[i] {
			return false
		}
	}
	return true
}

func hasAnyPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

func covered(path string) bool {
	for _, m := range guard.Mounts() {
		if m.Exact {
			if len(normalise(path)) == len(normalise(m.Prefix)) && prefixOf(m.Prefix, path) {
				return true
			}
			continue
		}
		if prefixOf(m.Prefix, path) {
			return true
		}
	}
	return false
}

func TestEveryProtectedRouteIsClassified(t *testing.T) {
	app := buildApp()

	seen := map[string]bool{}
	for _, routes := range app.Stack() {
		for _, r := range routes {
			path := r.Path
			if seen[path] {
				continue
			}
			seen[path] = true

			if publicRoutes[path] || anyAccount[path] {
				continue
			}
			if strings.HasPrefix(path, "/assets") {
				continue
			}
			if hasAnyPrefix(path, anyAccountPrefixes) {
				continue
			}
			if covered(path) {
				continue
			}
			t.Errorf("route %q is protected but matches no guard mount and is not listed as anyAccount; "+
				"add it to guard.Mounts() or to the anyAccount ledger in coverage_test.go", path)
		}
	}
}

func TestInstanceMountsCoverEveryPerInstanceShape(t *testing.T) {
	shapes := []string{
		"/api/valheim/instances/:num<int>/start",
		"/api/valheim/instances/:num<int>",
		"/api/valheim/:num<int>/mods/install",
		"/valheim/:num<int>/overview",
		"/api/minecraft/instances/:num<int>/restart",
		"/api/minecraft/:num<int>/configs/save",
		"/minecraft/:num<int>/overview",
	}
	for _, s := range shapes {
		if !covered(s) {
			t.Errorf("per-instance route shape %q is not covered by any guard mount", s)
		}
	}
}
