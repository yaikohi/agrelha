package server

import (
	mcaccess "agrelha/internal/app/access"
	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"agrelha/internal/infra/rcon"
	"context"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/admins"
	"agrelha/internal/app/mods"
	"agrelha/internal/infra/backups"
	"agrelha/internal/infra/content/mcversions"
	"agrelha/internal/infra/content/modpackindex"
	"agrelha/internal/infra/content/modrinth"
	"agrelha/internal/infra/content/thunderstore"
	"agrelha/internal/infra/gitops"
	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/store"
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
	mcMods           *mcaccess.ModManager
	mcAccess         *mcaccess.AccessManager
	mcRcon           *rcon.Client
	mcRconPool       *rcon.Pool
	mck8s            *k8s.Client
	mcInstances      *instances.InstanceManager
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

type Deps struct {
	Store       *store.Store
	K8s         *k8s.Client
	MCK8s       *k8s.Client
	Auth        ports.Auth
	Mods        *mods.Manager
	Admins      *admins.Manager
	Git         *gitops.Committer
	TS          *thunderstore.Client
	MR          *modrinth.Client
	MPI         *modpackindex.Client
	MCV         *mcversions.Client
	MCMods      *mcaccess.ModManager
	MCAccess    *mcaccess.AccessManager
	MCRcon      *rcon.Client
	MCRconPool  *rcon.Pool
	MCInstances *instances.InstanceManager
	StateStore  ports.StateStore
	Reconciler  ports.Reconciler
}

// New assembles the HTTP server from already-built dependencies. It performs no
// I/O and constructs no adapters: see internal/wiring for the composition root.
func New(cfg *config.Config, d Deps) *FiberServer {
	s := &FiberServer{
		App: fiber.New(fiber.Config{
			ServerHeader: "agrelha",
			AppName:      "agrelha",
		}),
		cfg:         cfg,
		store:       d.Store,
		k8s:         d.K8s,
		mck8s:       d.MCK8s,
		auth:        d.Auth,
		mods:        d.Mods,
		admins:      d.Admins,
		git:         d.Git,
		ts:          d.TS,
		mr:          d.MR,
		mpi:         d.MPI,
		mcv:         d.MCV,
		mcMods:      d.MCMods,
		mcAccess:    d.MCAccess,
		mcRcon:      d.MCRcon,
		mcRconPool:  d.MCRconPool,
		mcInstances: d.MCInstances,
		stateStore:  d.StateStore,
		reconciler:  d.Reconciler,
	}

	s.StartMinecraftScheduler(context.Background())

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
			InstanceStats: func(ctx context.Context, insts []domain.Instance) map[int]dashboardhttp.InstanceStat {
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
