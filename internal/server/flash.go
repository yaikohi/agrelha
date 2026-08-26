package server

import (
	"encoding/base64"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

const flashCookie = "agrelha_flash"

func setFlash(c *fiber.Ctx, kind, msg string) {
	v := base64.RawURLEncoding.EncodeToString([]byte(kind + "|" + msg))
	c.Cookie(&fiber.Cookie{
		Name: flashCookie, Value: v, HTTPOnly: true, Secure: true,
		SameSite: "Lax", Path: "/", Expires: time.Now().Add(5 * time.Minute),
	})
}

func takeFlash(c *fiber.Ctx) (kind, msg string) {
	raw := c.Cookies(flashCookie)
	if raw == "" {
		return "", ""
	}
	c.ClearCookie(flashCookie)
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return "", ""
	}
	k, m, _ := strings.Cut(string(b), "|")
	return k, m
}
