package shared

import (
	"context"

	"github.com/gofiber/fiber/v2"
)

const ThemeCookie = "agrelha_theme"

const (
	ThemeDark  = "dark"
	ThemeLight = "light"
)

type themeKey struct{}

func NormalizeTheme(v string) string {
	if v == ThemeLight {
		return ThemeLight
	}
	return ThemeDark
}

func WithTheme(ctx context.Context, theme string) context.Context {
	return context.WithValue(ctx, themeKey{}, NormalizeTheme(theme))
}

func ThemeOf(ctx context.Context) string {
	if t, ok := ctx.Value(themeKey{}).(string); ok {
		return t
	}
	return ThemeDark
}

func ThemeFromRequest(c *fiber.Ctx) string {
	return NormalizeTheme(c.Cookies(ThemeCookie))
}
