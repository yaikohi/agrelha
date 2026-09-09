package content

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/mods"
	"agrelha/internal/infra/content/thunderstore"
	"agrelha/internal/web/mdrender"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
)

// ModsPage renders the Valheim installed mods list and search results.
func (h *Handler) ModsPage(c *fiber.Ctx) error {
	var current []string
	if h.cfg.K8s != nil {
		if data, err := h.cfg.K8s.ConfigMapData(c.UserContext(), "valheim-mods"); err == nil {
			current = mods.Parse(data["mods.txt"])
		}
	}
	meta := map[string]thunderstore.SearchResult{}
	if h.cfg.TS != nil {
		for _, e := range current {
			key := pages.ModKey(e)
			if m, ok := h.cfg.TS.Get(key); ok {
				meta[key] = m
			}
		}
	}
	q := c.Query("q")
	var results []thunderstore.SearchResult
	indexing := false
	if h.cfg.TS != nil {
		if q != "" {
			results, _ = h.cfg.TS.Search(c.UserContext(), q, 25)
		}
		indexing = q != "" && !h.cfg.TS.Ready()
	}
	fk, fm := shared.TakeFlash(c)
	return shared.Render(c, pages.Mods(current, meta, q, results, h.cfg.Mods != nil, indexing, fk, fm))
}

// ModDetail renders detailed info and readme for a specific Valheim mod.
func (h *Handler) ModDetail(c *fiber.Ctx) error {
	ns, name := c.Params("namespace"), c.Params("name")
	key := ns + "/" + name
	ctx := c.UserContext()

	var entry thunderstore.SearchResult
	if h.cfg.TS != nil {
		entry, _ = h.cfg.TS.Get(key)
	}
	if entry.Owner == "" {
		entry.Owner = ns
	}
	if entry.Name == "" {
		entry.Name = name
	}

	version := entry.Version
	var depRaw []string
	if h.cfg.TS != nil {
		if v, deps, err := h.cfg.TS.LatestVersion(ctx, ns, name); err == nil {
			if version == "" {
				version = v
			}
			depRaw = deps
		}
	}

	var readmeHTML string
	if version != "" && h.cfg.Store != nil {
		md, hit, _ := h.cfg.Store.GetReadme(key, version)
		if !hit && h.cfg.TS != nil {
			if fetched, err := h.cfg.TS.Readme(ctx, ns, name, version); err == nil {
				md = fetched
				_ = h.cfg.Store.PutReadme(key, version, md)
			}
		}
		if md != "" {
			readmeHTML = mdrender.Render(md)
		}
	}

	installed := false
	if h.cfg.K8s != nil {
		if data, err := h.cfg.K8s.ConfigMapData(ctx, "valheim-mods"); err == nil {
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

	return shared.Render(c, pages.ModDetail(ns, name, entry, version, prettyDeps(depRaw), readmeHTML, tsURL, installed, h.cfg.Mods != nil))
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

// ModsInstall handles installing a Thunderstore mod and its dependencies.
func (h *Handler) ModsInstall(c *fiber.Ctx) error {
	if h.cfg.Mods == nil {
		shared.SetFlash(c, "err", "Declarative plane disabled — no Codeberg token configured.")
		return c.Redirect("/mods", fiber.StatusSeeOther)
	}
	ns, name := c.FormValue("namespace"), c.FormValue("name")
	if ns == "" || name == "" {
		shared.SetFlash(c, "err", "Namespace and name are required.")
		return c.Redirect("/mods", fiber.StatusSeeOther)
	}
	if h.cfg.TS == nil {
		shared.SetFlash(c, "err", "Thunderstore client not available.")
		return c.Redirect("/mods", fiber.StatusSeeOther)
	}
	ctx := c.UserContext()
	entries, err := h.cfg.TS.ResolveTree(ctx, ns, name)
	if err != nil {
		shared.SetFlash(c, "err", "Couldn't resolve "+ns+"/"+name+": "+err.Error())
		return c.Redirect("/mods", fiber.StatusSeeOther)
	}
	changed, err := h.cfg.Mods.Install(ctx, entries)
	if err != nil {
		shared.SetFlash(c, "err", "Install commit failed: "+err.Error())
		return c.Redirect("/mods", fiber.StatusSeeOther)
	}
	if h.cfg.Store != nil {
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "mod-install", ns+"/"+name)
	}
	if changed {
		h.cfg.ApplyAfterSync("valheim-mods", "mods.txt", func(txt string) bool {
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
		shared.SetFlash(c, "ok", installedMsg(ns, name, len(entries)-1))
	} else {
		shared.SetFlash(c, "ok", ns+"/"+name+" is already installed and up to date.")
	}
	return c.Redirect("/mods", fiber.StatusSeeOther)
}

func installedMsg(ns, name string, deps int) string {
	m := "Installed " + ns + "/" + name
	switch {
	case deps == 1:
		m += " (+1 dependency)"
	case deps > 1:
		m += " (+" + strconv.Itoa(deps) + " dependencies)"
	}
	return m + " — committed; the server will restart to apply."
}

// ModsRemove handles removing a mod from the declared mods list.
func (h *Handler) ModsRemove(c *fiber.Ctx) error {
	if h.cfg.Mods == nil {
		shared.SetFlash(c, "err", "Declarative plane disabled — no Codeberg token configured.")
		return c.Redirect("/mods", fiber.StatusSeeOther)
	}
	nsName := c.FormValue("mod")
	if nsName == "" {
		shared.SetFlash(c, "err", "No mod specified.")
		return c.Redirect("/mods", fiber.StatusSeeOther)
	}
	changed, err := h.cfg.Mods.Remove(c.UserContext(), nsName)
	if err != nil {
		shared.SetFlash(c, "err", "Remove commit failed: "+err.Error())
		return c.Redirect("/mods", fiber.StatusSeeOther)
	}
	if h.cfg.Store != nil {
		_ = h.cfg.Store.RecordAudit(h.cfg.Actor(c), "mod-remove", nsName)
	}
	if changed {
		h.cfg.ApplyAfterSync("valheim-mods", "mods.txt", func(txt string) bool {
			for _, e := range mods.Parse(txt) {
				if pages.ModKey(e) == nsName {
					return false
				}
			}
			return true
		})
		shared.SetFlash(c, "ok", "Removed "+nsName+" — committed; the server will restart to apply.")
	} else {
		shared.SetFlash(c, "ok", nsName+" was not installed.")
	}
	return c.Redirect("/mods", fiber.StatusSeeOther)
}
