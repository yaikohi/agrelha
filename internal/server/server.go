package server

import (
	"context"
	"log"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/admins"
	"agrelha/internal/auth"
	"agrelha/internal/config"
	"agrelha/internal/gitops"
	"agrelha/internal/ingest"
	"agrelha/internal/k8s"
	"agrelha/internal/mods"
	"agrelha/internal/store"
	"agrelha/internal/thunderstore"
)

type FiberServer struct {
	*fiber.App

	cfg    *config.Config
	store  *store.Store
	k8s    *k8s.Client
	auth   *auth.Authenticator
	mods   *mods.Manager
	admins *admins.Manager
	ts     *thunderstore.Client
}

func New(cfg *config.Config) *FiberServer {
	app := fiber.New(fiber.Config{
		ServerHeader: "agrelha",
		AppName:      "agrelha",
	})
	s := &FiberServer{App: app, cfg: cfg, ts: thunderstore.New(cfg.ThunderstoreAPI)}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	s.store = st

	// Declarative plane: needs a git token.
	if cfg.GitToken != "" {
		committer := &gitops.Committer{
			RepoURL: cfg.GitRepoURL, Branch: cfg.GitBranch,
			Username: cfg.GitUsername, Token: cfg.GitToken,
			AuthorName: cfg.GitAuthorName, AuthorEmail: cfg.GitAuthorEmail,
		}
		s.mods = mods.New(committer, cfg.ModsPath)
		s.admins = admins.New(committer, cfg.AdminsPath)
	} else {
		log.Printf("git token unset: declarative plane (mods/admins) disabled")
	}

	// Imperative plane + log ingester (in-cluster only).
	if c, err := k8s.New(cfg.ValheimNamespace, cfg.ValheimDeployment); err != nil {
		log.Printf("k8s client unavailable (dev?): %v", err)
	} else {
		s.k8s = c
		go ingest.Run(context.Background(), c, st)
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
