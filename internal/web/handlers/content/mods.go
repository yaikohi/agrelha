package content

import (
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/domain"
	"agrelha/internal/web/mdrender"
	"agrelha/internal/web/pages"
	"agrelha/internal/web/shared"
)

// ModDetail renders detailed info and readme for a specific Valheim mod.
func (h *Handler) ModDetail(c *fiber.Ctx) error {
	ns, name := c.Params("namespace"), c.Params("name")
	key := ns + "/" + name
	ctx := c.UserContext()

	var entry domain.ModSearchResult
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
	if version != "" && h.cfg.ReadmeCache != nil {
		md, hit, _ := h.cfg.ReadmeCache.GetReadme(key, version)
		if !hit && h.cfg.TS != nil {
			if fetched, err := h.cfg.TS.Readme(ctx, ns, name, version); err == nil {
				md = fetched
				_ = h.cfg.ReadmeCache.PutReadme(key, version, md)
			}
		}
		if md != "" {
			readmeHTML = mdrender.Render(md)
		}
	}

	tsURL := entry.FullURL
	if tsURL == "" {
		tsURL = fmt.Sprintf("https://thunderstore.io/c/valheim/p/%s/%s/", ns, name)
	}

	return shared.Render(c, pages.ModDetail(ns, name, entry, version, prettyDeps(depRaw), readmeHTML, tsURL))
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
