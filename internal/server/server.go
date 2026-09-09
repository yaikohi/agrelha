package server

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/admins"
	"agrelha/internal/app/ingest"
	"agrelha/internal/app/mods"
	"agrelha/internal/infra/auth/local"
	"agrelha/internal/infra/auth/oidc"
	"agrelha/internal/infra/backups"
	"agrelha/internal/infra/content/mcversions"
	"agrelha/internal/infra/content/modpackindex"
	"agrelha/internal/infra/content/modrinth"
	"agrelha/internal/infra/content/thunderstore"
	"agrelha/internal/infra/gitops"
	"agrelha/internal/infra/kube"
	argocd "agrelha/internal/infra/reconcile/argocd"
	compose "agrelha/internal/infra/reconcile/compose"
	dockerruntime "agrelha/internal/infra/runtime/docker"
	k8sruntime "agrelha/internal/infra/runtime/k8s"
	gitstate "agrelha/internal/infra/state/git"
	localstate "agrelha/internal/infra/state/local"
	"agrelha/internal/infra/state/unconfigured"
	"agrelha/internal/infra/store"
	"agrelha/internal/minecraft"
	"agrelha/internal/platform/config"
	"agrelha/internal/ports"
	"agrelha/internal/web/handlers/access"
	backupshttp "agrelha/internal/web/handlers/backups"
	consolehttp "agrelha/internal/web/handlers/console"
	contenthttp "agrelha/internal/web/handlers/content"
	dashboardhttp "agrelha/internal/web/handlers/dashboard"
	instanceshttp "agrelha/internal/web/handlers/instances"
	wizardhttp "agrelha/internal/web/handlers/wizard"
)

type FiberServer struct {
	*fiber.App

	cfg              *config.Config
	store            *store.Store
	k8s              *k8s.Client
	auth             ports.Auth
	mods             *mods.Manager
	admins           *admins.Manager
	git              *gitops.Committer
	ts               *thunderstore.Client
	mr               *modrinth.Client
	mpi              *modpackindex.Client
	mcv              *mcversions.Client
	mcMods           *minecraft.ModManager
	mcAccess         *minecraft.AccessManager
	mcRcon           *minecraft.RconClient
	mcRconPool       *minecraft.RconPool
	mck8s            *k8s.Client
	mcInstances      *minecraft.InstanceManager
	stateStore       ports.StateStore
	reconciler       ports.Reconciler
	accessHandler    *access.Handler
	backupsHandler   *backupshttp.Handler
	consoleHandler   *consolehttp.Handler
	dashboardHandler *dashboardhttp.Handler
	contentHandler   *contenthttp.Handler
	instancesHandler *instanceshttp.Handler
	wizardHandler    *wizardhttp.Handler

	bkMu   sync.Mutex
	bkInfo backups.Info
	bkOK   bool
	bkAt   time.Time

	pendMu  sync.Mutex
	pendSet map[string]bool
	pendAt  time.Time
}

type Option func(*FiberServer)

func WithAuth(a ports.Auth) Option {
	return func(s *FiberServer) {
		s.auth = a
	}
}

func WithStateStore(ss ports.StateStore) Option {
	return func(s *FiberServer) {
		s.stateStore = ss
	}
}

func WithReconciler(r ports.Reconciler) Option {
	return func(s *FiberServer) {
		s.reconciler = r
	}
}

func New(cfg *config.Config, opts ...Option) *FiberServer {
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

	if cfg.GitToken != "" && cfg.GitRepoURL != "" {
		s.reconciler = argocd.New()
		committer := &gitops.Committer{
			RepoURL: cfg.GitRepoURL, Branch: cfg.GitBranch,
			Username: cfg.GitUsername, Token: cfg.GitToken,
			AuthorName: cfg.GitAuthorName, AuthorEmail: cfg.GitAuthorEmail,
		}
		s.git = committer
		s.stateStore = gitstate.New(committer)
	} else if cfg.Runtime == "docker" || cfg.LocalStateDir != "" {
		s.reconciler = compose.New(compose.WithWorkDir(cfg.ComposeDir))
		stateDir := cfg.LocalStateDir
		if stateDir == "" {
			stateDir = "/data/state"
		}
		if localSS, err := localstate.New(stateDir, localstate.WithDB(st.DB())); err != nil {
			slog.Warn("local state store init failed", "err", err)
		} else {
			s.stateStore = localSS
		}
	} else {
		s.reconciler = argocd.New()
		s.stateStore = unconfigured.New()
		slog.Warn("GIT_REPO_URL/GIT_TOKEN unset: declarative plane is read-only")
	}

	if s.stateStore != nil {
		s.mods = mods.New(s.stateStore, cfg.ModsPath)
		s.admins = admins.New(s.stateStore, cfg.AdminsPath)
		s.mcMods = minecraft.NewModManager(s.stateStore, cfg.MinecraftModsPath)
		s.mcAccess = minecraft.NewAccessManager(s.stateStore, cfg.MinecraftAccessPath, s.mcRcon)
	}

	var rt ports.Runtime
	if cfg.Runtime == "docker" {
		rt = dockerruntime.New(dockerruntime.WithClient(dockerruntime.NewSocketClient(cfg.DockerSocket)))
	} else {
		if mcK8s, err := k8s.New(cfg.MinecraftNamespace, cfg.MinecraftDeployment); err != nil {
			slog.Warn("minecraft k8s client unavailable (dev?)", "err", err)
		} else {
			mcK8s.SetNodeSelector(cfg.GameNodeSelector)
			s.mck8s = mcK8s
		}
		rt = k8sruntime.New(s.mck8s)
	}

	if c, err := k8s.New(cfg.ValheimNamespace, cfg.ValheimDeployment); err != nil {
		slog.Warn("k8s client unavailable (dev?)", "err", err)
	} else {
		s.k8s = c
		go ingest.Run(context.Background(), c, st)
	}

	s.mcInstances = minecraft.NewInstanceManager(
		store.NewInstanceRepo(st), s.stateStore, rt,
		cfg.MCTotalBudgetGiB, cfg.MCMaxInstances, cfg.MCMaxRunning,
		cfg.MCInstancesPath,
		cfg.MCLBBaseIP,
		cfg.GameNodeSelector,
		cfg.MinecraftNamespace,
	)
	s.StartMinecraftScheduler(context.Background())

	if cfg.OIDCIssuer != "" {
		if a, err := oidc.New(context.Background(), cfg); err != nil {
			slog.Warn("oidc unavailable, falling back to local dev auth", "err", err)
			s.auth = oidc.NewDev(cfg)
		} else {
			s.auth = a
		}
	} else {
		users, err := st.ListUsers(context.Background())
		if err == nil && len(users) > 0 {
			slog.Info("oidc unset: local user accounts found, running with local authenticator", "users", len(users))
			s.auth = local.New(st, cfg.OIDCClientSecret)
		} else {
			slog.Info("oidc unset: running with local dev authenticator (click 'Admin Sign In' to authenticate)")
			s.auth = oidc.NewDev(cfg)
		}
	}

	for _, opt := range opts {
		opt(s)
	}

	s.ensureAccessHandler()
	s.ensureBackupsHandler()
	s.ensureConsoleHandler()
	s.ensureDashboardHandler()
	s.ensureContentHandler()
	s.ensureInstancesHandler()

	return s
}

// ensureWizardHandler builds the provisioning handler. Its Config is a narrower
// subset than the instances handler's — the wizard only needs these seven.
func (s *FiberServer) ensureWizardHandler() *wizardhttp.Handler {
	if s.wizardHandler == nil {
		s.wizardHandler = wizardhttp.New(wizardhttp.Config{
			Store:       s.store,
			MCK8s:       s.mck8s,
			MCInstances: s.mcInstances,
			MCV:         s.mcv,
			MPI:         s.mpi,
			MR:          s.mr,
			Actor:       s.actor,
		})
	}
	return s.wizardHandler
}

func (s *FiberServer) ensureInstancesHandler() *instanceshttp.Handler {
	if s.instancesHandler == nil {
		s.instancesHandler = instanceshttp.New(instanceshttp.Config{
			Cfg:                     s.cfg,
			Store:                   s.store,
			Git:                     s.git,
			MCK8s:                   s.mck8s,
			MCInstances:             s.mcInstances,
			MCRconPool:              s.mcRconPool,
			MCV:                     s.mcv,
			MPI:                     s.mpi,
			MR:                      s.mr,
			Actor:                   s.actor,
			ApplyMinecraftAfterSync: s.applyMinecraftAfterSync,
		})
	}
	return s.instancesHandler
}

func (s *FiberServer) ensureContentHandler() *contenthttp.Handler {
	if s.contentHandler == nil {
		s.contentHandler = contenthttp.New(contenthttp.Config{
			Cfg:            s.cfg,
			Store:          s.store,
			K8s:            s.k8s,
			Mods:           s.mods,
			Git:            s.git,
			TS:             s.ts,
			Actor:          s.actor,
			ApplyAfterSync: s.applyAfterSync,
			PendingActive:  s.pendingActive,
			SetPending:     s.setPending,
		})
	}
	return s.contentHandler
}

func (s *FiberServer) ensureDashboardHandler() *dashboardhttp.Handler {
	if s.dashboardHandler == nil {
		s.dashboardHandler = dashboardhttp.New(dashboardhttp.Config{
			Cfg:           s.cfg,
			Store:         s.store,
			K8s:           s.k8s,
			MCK8s:         s.mck8s,
			MCInstances:   s.mcInstances,
			MCAccess:      s.mcAccess,
			Auth:          s.auth,
			Actor:         s.actor,
			BackupInfo:    s.backupInfo,
			ModUpdates:    s.modUpdates,
			PendingActive: s.pendingActive,
			InstanceStats: func(ctx context.Context, insts []minecraft.Instance) map[int]dashboardhttp.InstanceStat {
				raw := s.instanceStats(ctx, insts)
				res := make(map[int]dashboardhttp.InstanceStat, len(raw))
				for k, v := range raw {
					res[k] = dashboardhttp.InstanceStat{
						Players:      v.Players,
						PlayersKnown: v.PlayersKnown,
						Uptime:       v.Uptime,
					}
				}
				return res
			},
		})
	}
	return s.dashboardHandler
}

func (s *FiberServer) ensureAccessHandler() *access.Handler {
	if s.accessHandler == nil {
		s.accessHandler = access.New(access.Config{
			Admins:         s.admins,
			MCAccess:       s.mcAccess,
			Store:          s.store,
			K8s:            s.k8s,
			MCK8s:          s.mck8s,
			StateStore:     s.stateStore,
			Cfg:            s.cfg,
			Actor:          s.actor,
			ApplyAfterSync: s.applyAfterSync,
		})
	}
	return s.accessHandler
}

func (s *FiberServer) ensureBackupsHandler() *backupshttp.Handler {
	if s.backupsHandler == nil {
		backupsDir := ""
		if s.cfg != nil {
			backupsDir = s.cfg.BackupsDir
		}
		s.backupsHandler = backupshttp.New(backupshttp.Config{
			BackupsDir:  backupsDir,
			MCInstances: s.mcInstances,
			MCK8s:       s.mck8s,
			RconPool:    s.mcRconPool,
			Store:       s.store,
			Actor:       s.actor,
		})
	}
	return s.backupsHandler
}

func (s *FiberServer) ensureConsoleHandler() *consolehttp.Handler {
	if s.consoleHandler == nil {
		s.consoleHandler = consolehttp.New(consolehttp.Config{
			K8s:         s.k8s,
			MCK8s:       s.mck8s,
			MCInstances: s.mcInstances,
			MCRconPool:  s.mcRconPool,
			Store:       s.store,
			Auth:        s.auth,
			Actor:       s.actor,
		})
	}
	return s.consoleHandler
}

func (s *FiberServer) isAdmin(c *fiber.Ctx) bool {
	if s.auth == nil {
		return true
	}
	return s.auth.IsAuthenticated(c)
}
