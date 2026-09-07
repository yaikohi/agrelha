package server

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/admins"
	"agrelha/internal/auth"
	"agrelha/internal/backups"
	"agrelha/internal/config"
	"agrelha/internal/gitops"
	"agrelha/internal/ingest"
	"agrelha/internal/k8s"
	"agrelha/internal/mcversions"
	"agrelha/internal/minecraft"
	"agrelha/internal/modpackindex"
	"agrelha/internal/modrinth"
	"agrelha/internal/mods"
	"agrelha/internal/store"
	"agrelha/internal/thunderstore"
)

type FiberServer struct {
	*fiber.App

	cfg      *config.Config
	store    *store.Store
	k8s      *k8s.Client
	auth     *auth.Authenticator
	mods     *mods.Manager
	admins   *admins.Manager
	git      *gitops.Committer
	ts       *thunderstore.Client
	mr       *modrinth.Client
	mpi      *modpackindex.Client
	mcv      *mcversions.Client
	mcMods   *minecraft.ModManager
	fabMods  *minecraft.ModManager
	mcSlot      *minecraft.SlotManager
	mcAccess    *minecraft.AccessManager
	mcRcon      *minecraft.RconClient
	mcRconPool  *minecraft.RconPool
	mck8s       *k8s.Client
	mcInstances *minecraft.InstanceManager

	bkMu   sync.Mutex
	bkInfo backups.Info
	bkOK   bool
	bkAt   time.Time

	pendMu  sync.Mutex
	pendSet map[string]bool
	pendAt  time.Time
}

func New(cfg *config.Config) *FiberServer {
	app := fiber.New(fiber.Config{
		ServerHeader: "agrelha",
		AppName:      "agrelha",
	})
	s := &FiberServer{App: app, cfg: cfg}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		slog.Error("store open failed", "path", cfg.DBPath, "err", err)
		os.Exit(1)
	}
	s.store = st

	s.ts = thunderstore.New(cfg.ThunderstoreAPI)
	if rows, fetchedAt, err := st.LoadModIndex(); err != nil {
		slog.Warn("mod index load failed", "err", err)
	} else if len(rows) > 0 {
		s.ts.Preload(rowsToResults(rows), fetchedAt)
		slog.Info("thunderstore index preloaded from cache", "packages", len(rows))
	}
	s.ts.OnRefresh = func(idx []thunderstore.SearchResult) {
		if err := st.SaveModIndex(resultsToRows(idx), time.Now()); err != nil {
			slog.Warn("mod index save failed", "err", err)
		}
	}
	go s.ts.WarmLoop(context.Background())

	s.mr = modrinth.New(cfg.ModrinthAPI)
	s.mpi = modpackindex.New("")
	s.mcv = mcversions.New("")
	if cfg.MinecraftRconPassword != "" {
		s.mcRcon = minecraft.NewRconClient(cfg.MinecraftRconAddr, cfg.MinecraftRconPassword, 3*time.Second)
		s.mcRconPool = minecraft.NewRconPool(cfg.MinecraftRconPassword, 3*time.Second)
	}

	if cfg.GitToken != "" {
		committer := &gitops.Committer{
			RepoURL: cfg.GitRepoURL, Branch: cfg.GitBranch,
			Username: cfg.GitUsername, Token: cfg.GitToken,
			AuthorName: cfg.GitAuthorName, AuthorEmail: cfg.GitAuthorEmail,
		}
		s.git = committer
		s.mods = mods.New(committer, cfg.ModsPath)
		s.admins = admins.New(committer, cfg.AdminsPath)
		s.mcMods = minecraft.NewModManager(committer, cfg.MinecraftModsPath)
		s.fabMods = minecraft.NewFabricModManager(committer, cfg.FabricModsPath)
		s.mcSlot = minecraft.NewSlotManager(committer, cfg.MinecraftSlotPath, cfg.MinecraftModsPath)
		s.mcAccess = minecraft.NewAccessManager(committer, cfg.MinecraftAccessPath, s.mcRcon)
	} else {
		slog.Warn("git token unset: declarative plane (mods/admins) disabled")
	}

	if c, err := k8s.New(cfg.ValheimNamespace, cfg.ValheimDeployment); err != nil {
		slog.Warn("k8s client unavailable (dev?)", "err", err)
	} else {
		s.k8s = c
		go ingest.Run(context.Background(), c, st)
	}

	if mcK8s, err := k8s.New(cfg.MinecraftNamespace, cfg.MinecraftDeployment); err != nil {
		slog.Warn("minecraft k8s client unavailable (dev?)", "err", err)
	} else {
		if cfg.FabricDeployment != "" {
			mcK8s.SetAltDeployment(cfg.FabricDeployment)
		}
		s.mck8s = mcK8s
	}

	s.mcInstances = minecraft.NewInstanceManager(
		st, s.git, s.mck8s,
		cfg.MCTotalBudgetGiB, cfg.MCMaxInstances, cfg.MCMaxRunning,
		"manifests/minecraft-modded",
	)

	if cfg.OIDCIssuer != "" {
		if a, err := auth.New(context.Background(), cfg); err != nil {
			slog.Warn("oidc unavailable (dev?)", "err", err)
		} else {
			s.auth = a
		}
	}

	return s
}
