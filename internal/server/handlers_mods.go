package server

import (
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/cmd/web/pages"
	"agrelha/internal/mdrender"
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
	meta := map[string]thunderstore.SearchResult{}
	for _, e := range current {
		key := pages.ModKey(e)
		if m, ok := s.ts.Get(key); ok {
			meta[key] = m
		}
	}
	q := c.Query("q")
	var results []thunderstore.SearchResult
	if q != "" {
		results, _ = s.ts.Search(c.UserContext(), q, 25)
	}
	indexing := q != "" && !s.ts.Ready()
	return render(c, pages.Mods(current, meta, q, results, s.mods != nil, indexing))
}

func (s *FiberServer) modDetail(c *fiber.Ctx) error {
	ns, name := c.Params("namespace"), c.Params("name")
	key := ns + "/" + name
	ctx := c.UserContext()

	entry, _ := s.ts.Get(key)
	if entry.Owner == "" {
		entry.Owner = ns
	}
	if entry.Name == "" {
		entry.Name = name
	}

	version := entry.Version
	var depRaw []string
	if v, deps, err := s.ts.LatestVersion(ctx, ns, name); err == nil {
		if version == "" {
			version = v
		}
		depRaw = deps
	}

	var readmeHTML string
	if version != "" {
		md, hit, _ := s.store.GetReadme(key, version)
		if !hit {
			if fetched, err := s.ts.Readme(ctx, ns, name, version); err == nil {
				md = fetched
				_ = s.store.PutReadme(key, version, md)
			}
		}
		if md != "" {
			readmeHTML = mdrender.Render(md)
		}
	}

	installed := false
	if s.k8s != nil {
		if data, err := s.k8s.ConfigMapData(ctx, "valheim-mods"); err == nil {
			for _, e := range mods.Parse(data["mods.txt"]) {
				if pages.ModKey(e) == key {
					installed = true
					break
				}
			}
		}
	}

	tsURL := entry.FullURL
	if tsURL == "" {
		tsURL = fmt.Sprintf("https://thunderstore.io/c/valheim/p/%s/%s/", ns, name)
	}

	return render(c, pages.ModDetail(ns, name, entry, version, prettyDeps(depRaw), readmeHTML, tsURL, installed, s.mods != nil))
}

func prettyDeps(raw []string) []string {
	var out []string
	for _, d := range raw {
		parts := strings.Split(d, "-")
		if len(parts) < 3 {
			continue
		}
		n := strings.Join(parts[1:len(parts)-1], "-")
		if strings.HasPrefix(strings.ToLower(n), "bepinexpack") {
			continue
		}
		out = append(out, fmt.Sprintf("%s/%s (%s)", parts[0], n, parts[len(parts)-1]))
	}
	return out
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
	nsName := c.FormValue("mod")
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
