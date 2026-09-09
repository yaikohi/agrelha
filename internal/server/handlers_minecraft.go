package server

import (
	"context"
	"slices"
	"strings"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/infra/content/mcversions"
)

func (s *FiberServer) defaultMCVersion(ctx context.Context) string {
	if s.mcv == nil {
		return mcversions.FallbackLatest
	}
	return s.mcv.Latest(ctx)
}

func (s *FiberServer) mcVersionChoices(ctx context.Context, current string) []string {
	var out []string
	if s.mcv != nil {
		out = s.mcv.Releases(ctx, 15)
	}
	if len(out) == 0 {
		out = []string{mcversions.FallbackLatest}
	}
	if current == "" {
		return out
	}
	if slices.Contains(out, current) {
		return out
	}
	return append([]string{current}, out...)
}

func loaderMatches(loaders []string, target string) bool {
	if len(loaders) == 0 {
		return true
	}
	for _, l := range loaders {
		l = strings.ToLower(strings.TrimSpace(l))
		if l == target || (target == "neoforge" && l == "forge") {
			return true
		}
	}
	return false
}

func (s *FiberServer) mcAccessPage(c *fiber.Ctx) error {
	return s.ensureAccessHandler().MCAccessPage(c)
}

func (s *FiberServer) mcAccessGrantOp(c *fiber.Ctx) error {
	return s.ensureAccessHandler().MCAccessGrantOp(c)
}

func (s *FiberServer) mcAccessRevokeOp(c *fiber.Ctx) error {
	return s.ensureAccessHandler().MCAccessRevokeOp(c)
}

func (s *FiberServer) mcAccessAddWhitelist(c *fiber.Ctx) error {
	return s.ensureAccessHandler().MCAccessAddWhitelist(c)
}

func (s *FiberServer) mcAccessRemoveWhitelist(c *fiber.Ctx) error {
	return s.ensureAccessHandler().MCAccessRemoveWhitelist(c)
}

func (s *FiberServer) mcAccessWhitelistToggle(c *fiber.Ctx) error {
	return s.ensureAccessHandler().MCAccessWhitelistToggle(c)
}
