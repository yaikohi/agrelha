package server

import (
	"github.com/gofiber/fiber/v2"

	"agrelha/cmd/web/pages"
	"agrelha/internal/mods"
	"agrelha/internal/thunderstore"
)

func (s *FiberServer) modsPage(c *fiber.Ctx) error {
	var current []string
	if s.k8s != nil {
		if data, err := s.k8s.ConfigMapData(c.UserContext(), "valheim-mods"); err == nil {
			current = mods.Parse(data["mods.txt"])
		}
	}
	q := c.Query("q")
	var results []thunderstore.SearchResult
	if q != "" && s.ts != nil {
		results, _ = s.ts.Search(c.UserContext(), q, 25)
	}
	return render(c, pages.Mods(current, q, results, s.mods != nil))
}

func (s *FiberServer) modsInstall(c *fiber.Ctx) error {
	if s.mods == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "declarative plane disabled (no git token)")
	}
	ns, name := c.FormValue("namespace"), c.FormValue("name")
	if ns == "" || name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "namespace and name required")
	}
	ctx := c.UserContext()
	entries, err := s.ts.ResolveTree(ctx, ns, name)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "resolve deps: "+err.Error())
	}
	changed, err := s.mods.Install(ctx, entries)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	_ = s.store.RecordAudit(s.actor(c), "mod-install", ns+"/"+name)
	if changed {
		s.applyAfterSync("valheim-mods", "mods.txt", func(txt string) bool {
			present := map[string]bool{}
			for _, e := range mods.Parse(txt) {
				present[e] = true
			}
			for _, e := range entries {
				if !present[e] {
					return false
				}
			}
			return true
		})
	}
	return c.Redirect("/mods", fiber.StatusSeeOther)
}

func (s *FiberServer) modsRemove(c *fiber.Ctx) error {
	if s.mods == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "declarative plane disabled (no git token)")
	}
	nsName := c.FormValue("mod") // "namespace/name"
	if nsName == "" {
		return fiber.NewError(fiber.StatusBadRequest, "mod required")
	}
	changed, err := s.mods.Remove(c.UserContext(), nsName)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	_ = s.store.RecordAudit(s.actor(c), "mod-remove", nsName)
	if changed {
		s.applyAfterSync("valheim-mods", "mods.txt", func(txt string) bool {
			for _, e := range mods.Parse(txt) {
				if pages.ModKey(e) == nsName {
					return false
				}
			}
			return true
		})
	}
	return c.Redirect("/mods", fiber.StatusSeeOther)
}
