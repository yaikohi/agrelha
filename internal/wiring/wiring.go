package wiring

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"time"

	mcaccess "agrelha/internal/app/access"
	"agrelha/internal/app/admins"
	"agrelha/internal/app/games/minecraft"
	"agrelha/internal/app/games/valheim"
	"agrelha/internal/app/ingest"
	"agrelha/internal/app/instances"
	"agrelha/internal/app/modpack"
	"agrelha/internal/app/mods"
	"agrelha/internal/domain"
	"agrelha/internal/infra/auth/local"
	"agrelha/internal/infra/auth/oidc"
	"agrelha/internal/infra/content/mcversions"
	"agrelha/internal/infra/content/modpackindex"
	"agrelha/internal/infra/content/modrinth"
	"agrelha/internal/infra/content/thunderstore"
	"agrelha/internal/infra/gitops"
	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/manifests"
	valheimmanifests "agrelha/internal/infra/manifests/valheim"
	"agrelha/internal/infra/rcon"
	argocd "agrelha/internal/infra/reconcile/argocd"
	compose "agrelha/internal/infra/reconcile/compose"
	dockerruntime "agrelha/internal/infra/runtime/docker"
	k8sruntime "agrelha/internal/infra/runtime/k8s"
	gitstate "agrelha/internal/infra/state/git"
	localstate "agrelha/internal/infra/state/local"
	"agrelha/internal/infra/state/unconfigured"
	"agrelha/internal/infra/store"
	"agrelha/internal/platform/config"
	"agrelha/internal/ports"
)

// Deps bundles all constructed infrastructure adapters and application services.
type Deps struct {
	Store            *store.Store
	K8s              *k8s.Client
	MCK8s            *k8s.Client
	Auth             ports.Auth
	ValheimGame      ports.Game
	MinecraftGame    ports.Game
	ValheimRuntime   ports.Runtime
	ValheimRef       ports.ServerRef
	MCRuntime        ports.Runtime
	MCRef            ports.ServerRef
	Mods             *mods.Manager
	Admins           *admins.Manager
	Git              *gitops.Committer
	TS               *thunderstore.Client
	MR               *modrinth.Client
	MPI              *modpackindex.Client
	MCV              *mcversions.Client
	MCMods           *mcaccess.ModManager
	MCAccess         *mcaccess.AccessManager
	MCRcon           *rcon.Client
	MCRconPool       *rcon.Pool
	MCInstances      *instances.InstanceManager
	ValheimInstances *instances.InstanceManager
	StateStore       ports.StateStore
	Reconciler       ports.Reconciler
}

// Build is the composition root: it is the only place that decides which
// adapter satisfies which port. Everything it returns is already constructed,
// so server handlers perform no I/O and can be handed fakes in a test.
func Build(ctx context.Context, cfg *config.Config) (Deps, error) {
	var d Deps

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return d, fmt.Errorf("open store at %s: %w", cfg.DBPath, err)
	}
	d.Store = st

	d.TS = buildThunderstore(ctx, cfg, st)
	d.MR = modrinth.New(cfg.ModrinthAPI)
	d.MPI = modpackindex.New("")
	d.MCV = mcversions.New("")

	if cfg.MinecraftRconPassword != "" {
		d.MCRcon = rcon.NewClient(cfg.MinecraftRconAddr, cfg.MinecraftRconPassword, 3*time.Second)
		d.MCRconPool = rcon.NewPool(cfg.MinecraftRconPassword, 3*time.Second)
	}

	d.StateStore, d.Reconciler, d.Git = buildDeclarativePlane(cfg, st)

	if d.StateStore != nil {
		d.Mods = mods.New(d.StateStore, cfg.ModsPath)
		d.Admins = admins.New(d.StateStore, cfg.AdminsPath,
			admins.WithAudit(st),
		)
		d.MCMods = mcaccess.NewModManager(d.StateStore, cfg.MinecraftModsPath)

		var console ports.Console
		if d.MCRcon != nil {
			console = d.MCRcon
		}
		d.MCAccess = mcaccess.NewAccessManager(d.StateStore, cfg.MinecraftAccessPath, console,
			mcaccess.WithAudit(st),
		)
	}

	rt := buildRuntime(cfg, &d)

	if c, err := k8s.New(cfg.ValheimNamespace, cfg.ValheimDeployment); err != nil {
		slog.Warn("k8s client unavailable (dev?)", "err", err)
	} else {
		c.SetNodeSelector(cfg.GameNodeSelector)
		d.K8s = c
		go ingest.Run(ctx, c, st)
	}

	var instOpts []instances.Option
	instOpts = append(instOpts,
		instances.WithAudit(st),
		instances.WithEvent(st),
		instances.WithBackupsDir(cfg.BackupsDir),
		instances.WithGlobalConfigsPath(cfg.MinecraftConfigsPath),
	)
	if d.MR != nil {
		instOpts = append(instOpts, instances.WithDependencyResolver(d.MR.ResolveRequiredDependencies))
	}
	if d.MCRconPool != nil {
		rconExec := func(inst domain.Instance, cmd string) (string, error) {
			addr := fmt.Sprintf("%s.%s.svc.cluster.local:25575", inst.ServiceName(), cfg.MinecraftNamespace)
			res, err := d.MCRconPool.ClientFor(addr).Execute(cmd)
			if err != nil && inst.LBIP != "" {
				res, err = d.MCRconPool.ClientFor(inst.LBIP + ":25575").Execute(cmd)
			}
			return res, err
		}
		instOpts = append(instOpts,
			instances.WithPreStopHook(func(ctx context.Context, inst domain.Instance) {
				_, _ = rconExec(inst, "/say Server stopping in 5 seconds...")
			}),
			instances.WithPreDeleteHook(func(ctx context.Context, inst domain.Instance) {
				_, _ = rconExec(inst, "/say Server being deleted...")
			}),
			instances.WithTelemetryProvider(func(ctx context.Context, inst domain.Instance) (int, bool) {
				res, err := rconExec(inst, "/list")
				if err != nil {
					return 0, false
				}
				return len(mcaccess.ParsePlayerList(res)), true
			}),
			instances.WithCommandExecutor(func(ctx context.Context, inst domain.Instance, cmd string) (string, error) {
				return rconExec(inst, cmd)
			}),
		)
	}
	if d.MCK8s != nil {
		instOpts = append(instOpts,
			instances.WithJobRunner(d.MCK8s),
			instances.WithConfigsReader(func(ctx context.Context, num int) (map[string]string, error) {
				inst, err := d.MCInstances.GetInstance(ctx, num)
				if err != nil {
					return nil, err
				}
				return d.MCK8s.ConfigMapData(ctx, inst.ConfigsCMName())
			}),
			instances.WithModsReader(func(ctx context.Context, num int) ([]string, error) {
				inst, err := d.MCInstances.GetInstance(ctx, num)
				if err != nil {
					return nil, err
				}
				cm, err := d.MCK8s.ConfigMapData(ctx, inst.ModsCMName())
				if err != nil {
					return nil, err
				}
				modsTxt := cm["mods.txt"]
				var lines []string
				for line := range strings.SplitSeq(modsTxt, "\n") {
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "#") {
						lines = append(lines, line)
					}
				}
				return lines, nil
			}),
			instances.WithGlobalConfigsReader(func(ctx context.Context) (map[string]string, error) {
				return d.MCK8s.ConfigMapData(ctx, "minecraft-modded-configs")
			}),
		)
	}

	d.MCInstances = instances.NewInstanceManager(
		store.NewInstanceRepo(st), d.StateStore, rt,
		cfg.MCTotalBudgetGiB, cfg.MCMaxInstances, cfg.MCMaxRunning,
		cfg.MCInstancesPath,
		cfg.MCLBBaseIP,
		manifests.New(cfg.GameNodeSelector, cfg.MinecraftNamespace),
		cfg.MinecraftNamespace,
		instOpts...,
	)

	d.Auth = buildAuth(ctx, cfg, st)

	var valheimRuntime ports.Runtime
	if cfg.Runtime == "docker" {
		valheimRuntime = rt
	} else if d.K8s != nil {
		valheimRuntime = k8sruntime.New(d.K8s)
	}
	d.ValheimRuntime = valheimRuntime
	d.ValheimRef = ports.ServerRef{Name: cfg.ValheimDeployment, Scope: cfg.ValheimNamespace}

	var valheimInstOpts []instances.Option
	valheimInstOpts = append(valheimInstOpts,
		instances.WithGameID(domain.GameValheim),
		instances.WithAudit(st),
		instances.WithEvent(st),
		instances.WithBackupsDir(cfg.BackupsDir),
		instances.WithGlobalConfigsPath(cfg.ModConfigsPath),
	)
	if st != nil {
		valheimInstOpts = append(valheimInstOpts,
			instances.WithTelemetryProvider(func(ctx context.Context, inst domain.Instance) (int, bool) {
				count, err := st.CountOnline()
				return count, err == nil
			}),
		)
	}
	if d.K8s != nil {
		valheimInstOpts = append(valheimInstOpts,
			instances.WithJobRunner(d.K8s),
			instances.WithConfigsReader(func(ctx context.Context, num int) (map[string]string, error) {
				inst, err := d.ValheimInstances.GetInstance(ctx, num)
				if err != nil {
					return nil, err
				}
				return d.K8s.ConfigMapData(ctx, inst.ConfigsCMName())
			}),
			instances.WithModsReader(func(ctx context.Context, num int) ([]string, error) {
				inst, err := d.ValheimInstances.GetInstance(ctx, num)
				if err != nil {
					return nil, err
				}
				cm, err := d.K8s.ConfigMapData(ctx, inst.ModsCMName())
				if err != nil {
					return nil, err
				}
				modsTxt := cm["mods.txt"]
				var lines []string
				for line := range strings.SplitSeq(modsTxt, "\n") {
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "#") {
						lines = append(lines, line)
					}
				}
				return lines, nil
			}),
			instances.WithGlobalConfigsReader(func(ctx context.Context) (map[string]string, error) {
				return d.K8s.ConfigMapData(ctx, "valheim-mod-configs")
			}),
		)
	}
	valheimInstOpts = append(valheimInstOpts,
		instances.WithServerRefResolver(func(inst domain.Instance) ports.ServerRef {
			depName := inst.DeploymentName()
			if inst.Number == 1 && cfg.ValheimDeployment != "" && d.K8s != nil {
				if _, _, err := d.K8s.DeploymentReplicas(context.Background(), depName); err != nil {
					return ports.ServerRef{Name: cfg.ValheimDeployment, Scope: cfg.ValheimNamespace}
				}
			}
			return ports.ServerRef{Name: depName, Scope: cfg.ValheimNamespace}
		}),
	)

	d.ValheimInstances = instances.NewInstanceManager(
		store.NewValheimInstanceRepo(st), d.StateStore, valheimRuntime,
		cfg.ValheimTotalBudgetGiB, cfg.ValheimMaxInstances, cfg.ValheimMaxRunning,
		cfg.ValheimInstancesPath,
		cfg.ValheimLBBaseIP,
		valheimmanifests.New(cfg.GameNodeSelector, cfg.ValheimNamespace),
		cfg.ValheimNamespace,
		valheimInstOpts...,
	)

	d.MCRuntime = rt
	d.MCRef = ports.ServerRef{Name: cfg.MinecraftDeployment, Scope: cfg.MinecraftNamespace}

	d.ValheimGame = valheim.New(
		valheim.WithRuntime(valheimRuntime, d.ValheimRef),
		valheim.WithPlayerCount(st.CountOnline),
		valheim.WithBundleSource(func(ctx context.Context) ([]string, map[string]string, error) {
			var entries []string
			configs := map[string]string{}
			if d.K8s != nil {
				if data, err := d.K8s.ConfigMapData(ctx, "valheim-mods"); err == nil {
					entries = mods.Parse(data["mods.txt"])
				}
				if cfgData, err := d.K8s.ConfigMapData(ctx, "valheim-mod-configs"); err == nil {
					maps.Copy(configs, cfgData)
				}
			}
			return entries, configs, nil
		}),
	)

	d.MinecraftGame = minecraft.New(
		minecraft.WithRuntime(rt, ports.ServerRef{Name: cfg.MinecraftDeployment, Scope: cfg.MinecraftNamespace}),
		minecraft.WithPlayerCount(func(ctx context.Context) (int, error) {
			if d.MCAccess != nil {
				players, err := d.MCAccess.OnlinePlayers()
				if err != nil {
					return 0, err
				}
				return len(players), nil
			}
			return 0, nil
		}),
		minecraft.WithActiveInstance(func(ctx context.Context) (domain.Loader, string) {
			if d.MCInstances != nil {
				if insts, err := d.MCInstances.ListInstances(ctx); err == nil && len(insts) > 0 {
					inst := insts[0]
					pack := ""
					if inst.PackDefined() && inst.Pack != nil {
						pack = inst.Pack.Name
					}
					return inst.Loader, pack
				}
			}
			return domain.LoaderNeoForge, ""
		}),
		minecraft.WithBundleBuilder(func(ctx context.Context, inst domain.Instance) (domain.Bundle, error) {
			var slugs []string
			cfgFiles := make(map[string]string)
			if d.MCK8s != nil {
				if data, err := d.MCK8s.ConfigMapData(ctx, inst.ModsCMName()); err == nil {
					if modsTxt, ok := data["mods.txt"]; ok {
						for line := range strings.SplitSeq(modsTxt, "\n") {
							line = strings.TrimSpace(line)
							if line != "" && !strings.HasPrefix(line, "#") {
								slugs = append(slugs, strings.TrimSuffix(line, "?"))
							}
						}
					}
				}
				if cfgData, err := d.MCK8s.ConfigMapData(ctx, inst.ConfigsCMName()); err == nil {
					cfgFiles = cfgData
				}
			}

			loader := string(inst.Loader)
			if loader == "" {
				loader = string(domain.LoaderNeoForge)
			}
			mcVer := inst.MCVersion
			if mcVer == "" {
				mcVer = "1.21.1"
			}

			mrpackBytes, err := modpack.BuildMrpack(ctx, d.MR, inst.Name, mcVer, loader, "", slugs, cfgFiles)
			if err != nil {
				return domain.Bundle{}, fmt.Errorf("build mrpack: %w", err)
			}

			return domain.Bundle{
				Filename:    fmt.Sprintf("%s-%s.mrpack", inst.Slug, mcVer),
				ContentType: "application/x-modrinth-modpack+zip",
				Data:        mrpackBytes,
			}, nil
		}),
	)

	return d, nil
}

func buildThunderstore(ctx context.Context, cfg *config.Config, st *store.Store) *thunderstore.Client {
	ts := thunderstore.New(cfg.ThunderstoreAPI)

	if rows, fetchedAt, err := st.LoadModIndex(); err != nil {
		slog.Warn("mod index load failed", "err", err)
	} else if len(rows) > 0 {
		ts.Preload(store.RowsToResults(rows), fetchedAt)
		slog.Info("thunderstore index preloaded from cache", "packages", len(rows))
	}

	ts.OnRefresh = func(idx []thunderstore.SearchResult) {
		if err := st.SaveModIndex(store.ResultsToRows(idx), time.Now()); err != nil {
			slog.Warn("mod index save failed", "err", err)
		}
	}
	go ts.WarmLoop(ctx)

	return ts
}

func buildDeclarativePlane(cfg *config.Config, st *store.Store) (ports.StateStore, ports.Reconciler, *gitops.Committer) {
	if cfg.GitToken != "" && cfg.GitRepoURL != "" {
		committer := &gitops.Committer{
			RepoURL: cfg.GitRepoURL, Branch: cfg.GitBranch,
			Username: cfg.GitUsername, Token: cfg.GitToken,
			AuthorName: cfg.GitAuthorName, AuthorEmail: cfg.GitAuthorEmail,
		}
		return gitstate.New(committer), argocd.New(), committer
	}

	if cfg.Runtime == "docker" || cfg.LocalStateDir != "" {
		stateDir := cfg.LocalStateDir
		if stateDir == "" {
			stateDir = "/data/state"
		}
		reconciler := compose.New(compose.WithWorkDir(cfg.ComposeDir))
		localSS, err := localstate.New(stateDir, localstate.WithDB(st.DB()))
		if err != nil {
			slog.Warn("local state store init failed", "err", err)
			return nil, reconciler, nil
		}
		return localSS, reconciler, nil
	}

	slog.Warn("GIT_REPO_URL/GIT_TOKEN unset: declarative plane is read-only")
	return unconfigured.New(), argocd.New(), nil
}

func buildRuntime(cfg *config.Config, d *Deps) ports.Runtime {
	if cfg.Runtime == "docker" {
		return dockerruntime.New(dockerruntime.WithClient(dockerruntime.NewSocketClient(cfg.DockerSocket)))
	}

	if mcK8s, err := k8s.New(cfg.MinecraftNamespace, cfg.MinecraftDeployment); err != nil {
		slog.Warn("minecraft k8s client unavailable (dev?)", "err", err)
	} else {
		mcK8s.SetNodeSelector(cfg.GameNodeSelector)
		d.MCK8s = mcK8s
	}
	return k8sruntime.New(d.MCK8s)
}

func buildAuth(ctx context.Context, cfg *config.Config, st *store.Store) ports.Auth {
	oidcCfg := oidc.Config{
		Issuer:        cfg.OIDCIssuer,
		ClientID:      cfg.OIDCClientID,
		ClientSecret:  cfg.OIDCClientSecret,
		RedirectURL:   cfg.OIDCRedirectURL,
		PostLogoutURL: cfg.OIDCPostLogoutURL,
		AllowedEmail:  cfg.AllowedEmail,
	}
	if cfg.OIDCIssuer != "" {
		a, err := oidc.New(ctx, oidcCfg)
		if err != nil {
			slog.Warn("oidc unavailable, falling back to local dev auth", "err", err)
			return oidc.NewDev(oidcCfg)
		}
		return a
	}

	users, err := st.ListUsers(ctx)
	if err == nil && len(users) > 0 {
		slog.Info("oidc unset: local user accounts found, running with local authenticator", "users", len(users))
		return local.New(st, cfg.OIDCClientSecret)
	}

	slog.Info("oidc unset: running with local dev authenticator (click 'Admin Sign In' to authenticate)")
	return oidc.NewDev(oidcCfg)
}
