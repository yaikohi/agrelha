package server

import (
	"context"
	"log"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/auth"
	"agrelha/internal/config"
	"agrelha/internal/k8s"
	"agrelha/internal/store"
)

type FiberServer struct {
	*fiber.App

	cfg   *config.Config
	store *store.Store
	k8s   *k8s.Client
	auth  *auth.Authenticator
}

func New(cfg *config.Config) *FiberServer {
	app := fiber.New(fiber.Config{
		ServerHeader: "agrelha",
		AppName:      "agrelha",
	})

	s := &FiberServer{App: app, cfg: cfg}

	// SQLite is required — fail loud if it can't open.
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	s.store = st

	// k8s + auth are network-dependent; in local dev (no in-cluster SA / no
	// reachable issuer) we log and continue so /healthz + the shell still serve.
	if c, err := k8s.New(cfg.ValheimNamespace, cfg.ValheimDeployment); err != nil {
		log.Printf("k8s client unavailable (dev?): %v", err)
	} else {
		s.k8s = c
	}
	if cfg.OIDCIssuer != "" {
		if a, err := auth.New(context.Background(), cfg); err != nil {
			log.Printf("oidc unavailable (dev?): %v", err)
		} else {
			s.auth = a
		}
	}

	return s
}
