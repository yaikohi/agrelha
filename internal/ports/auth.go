package ports

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
)

// User represents an authenticated local or external operator account.
type User struct {
	Username  string
	Email     string
	CreatedAt time.Time
}

// UserStore manages persistence of local user credentials.
type UserStore interface {
	GetUser(ctx context.Context, username string) (*User, string, error) // user and passwordHash
	CreateUser(ctx context.Context, user User, passwordHash string) error
	ListUsers(ctx context.Context) ([]User, error)
	DeleteUser(ctx context.Context, username string) error
}

// Auth defines the web authentication and session lifecycle contract.
type Auth interface {
	IsAuthenticated(c *fiber.Ctx) bool
	Middleware() fiber.Handler
	Login(c *fiber.Ctx) error
	Callback(c *fiber.Ctx) error
	Logout(c *fiber.Ctx) error
}
