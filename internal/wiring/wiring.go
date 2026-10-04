package wiring

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"time"

	mcaccess "agrelha/internal/app/access"
	"agrelha/internal/app/admins"
	"agrelha/internal/app/authz"
	appbepinex "agrelha/internal/app/bepinex"
	"agrelha/internal/app/capacity"
	"agrelha/internal/app/games"
	"agrelha/internal/app/games/minecraft"
	"agrelha/internal/app/games/valheim"
	"agrelha/internal/app/health"
	"agrelha/internal/app/ingest"
	"agrelha/internal/app/instances"
	"agrelha/internal/app/modpack"
	"agrelha/internal/app/mods"
	"agrelha/internal/app/modupdates"
	"agrelha/internal/app/occupancy"
	"agrelha/internal/app/requests"
	"agrelha/internal/app/restarts"
	"agrelha/internal/domain"
	"agrelha/internal/infra/auth/local"
	"agrelha/internal/infra/auth/oidc"
	infrabackups "agrelha/internal/infra/backups"
	"agrelha/internal/infra/content/curseforge"
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
	zitadelroles "agrelha/internal/infra/roles/zitadel"
	dockerruntime "agrelha/internal/infra/runtime/docker"
	k8sruntime "agrelha/internal/infra/runtime/k8s"
	gitstate "agrelha/internal/infra/state/git"
	localstate "agrelha/internal/infra/state/local"
	"agrelha/internal/infra/state/unconfigured"
	"agrelha/internal/infra/store"
	"agrelha/internal/infra/valheimstatus"
	"agrelha/internal/platform/config"
	"agrelha/internal/ports"
)

var newK8sClient = k8s.New

// Deps bundles all constructed infrastructure adapters and application services.
type Deps struct {
	Store            *store.Store
	K8s              *k8s.Client
	MCK8s            *k8s.Client
	Auth             ports.Auth
	Authz            *authz.Service
	Capacity         *capacity.Service
	Requests         *requests.Service
	Games            *games.Registry
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
	CF               *curseforge.Client
	MPI              *modpackindex.Client
	MCV              *mcversions.Client
	MCMods           *mcaccess.ModManager
	MCAccess         *mcaccess.AccessManager
	MCRcon           *rcon.Client
	MCRconPool       *rcon.Pool
	MCInstances      *instances.InstanceManager
	ValheimInstances *instances.InstanceManager
	ValheimConfigs   *appbepinex.Service
	ValheimRestarts  *restarts.Queue
	ValheimStatus    *valheimstatus.Client
	ValheimOccupancy *occupancy.Tracker
	ModUpdates       *modupdates.Checker
	MCModUpdates     *modupdates.Checker
	StateStore       ports.StateStore
	Reconciler       ports.Reconciler
}

var buildMrpack = modpack.BuildMrpack

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

	if c, err := newK8sClient(cfg.ValheimNamespace, cfg.ValheimDeployment); err != nil {
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
	// New(..., "") returns nil, so an unset key leaves CurseForge absent rather
	// than broken: search shows nothing and installs are refused with a reason.
	d.CF = curseforge.New(cfg.CurseForgeAPI, cfg.CurseForgeAPIKey)
	if d.CF.Ready() {
		instOpts = append(instOpts, instances.WithCurseForgeResolver(d.CF.Resolve))
	} else {
		slog.Info("curseforge: no API key configured, CurseForge mods unavailable")
	}
	if d.MCRconPool != nil {
		rconExec := func(inst domain.Instance, cmd string) (string, error) {
			addr := fmt.Sprintf("%s.%s.svc.cluster.local:25575", inst.ServiceName(domain.MinecraftProfile), cfg.MinecraftNamespace)
			res, err := d.MCRconPool.ClientFor(addr).Execute(cmd)
			if err != nil && inst.LBIP != "" {
				target := inst.LBIP
				if !strings.Contains(target, ":") {
					target = target + ":25575"
				}
				res, err = d.MCRconPool.ClientFor(target).Execute(cmd)
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
				if err != nil || inst == nil {
					if err == nil {
						err = fmt.Errorf("instance %d not found", num)
					}
					return nil, err
				}
				return d.MCK8s.ConfigMapData(ctx, inst.ConfigsCMName(domain.MinecraftProfile))
			}),
			instances.WithModsReader(func(ctx context.Context, num int) ([]string, error) {
				inst, err := d.MCInstances.GetInstance(ctx, num)
				if err != nil || inst == nil {
					if err == nil {
						err = fmt.Errorf("instance %d not found", num)
					}
					return nil, err
				}
				cm, err := d.MCK8s.ConfigMapData(ctx, inst.ModsCMName(domain.MinecraftProfile))
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
			// The CurseForge half of the same ConfigMap. Absent for any world with
			// no CurseForge mods, which is not an error.
			instances.WithCurseForgeReader(func(ctx context.Context, num int) ([]string, error) {
				inst, err := d.MCInstances.GetInstance(ctx, num)
				if err != nil || inst == nil {
					if err == nil {
						err = fmt.Errorf("instance %d not found", num)
					}
					return nil, err
				}
				cm, err := d.MCK8s.ConfigMapData(ctx, inst.ModsCMName(domain.MinecraftProfile))
				if err != nil {
					return nil, err
				}
				var lines []string
				for _, ref := range domain.SplitModLines(cm["curseforge.txt"], domain.ProviderCurseForge) {
					lines = append(lines, ref.Entry())
				}
				return lines, nil
			}),
			instances.WithGlobalConfigsReader(func(ctx context.Context) (map[string]string, error) {
				return d.MCK8s.ConfigMapData(ctx, "minecraft-modded-configs")
			}),
		)
	}

	d.Capacity = capacity.New(st, st, capacity.WithAudit(st), capacity.WithGlobals(st))
	for _, profile := range domain.Profiles() {
		err := d.Capacity.Seed(ctx, profile.ID, defaultGameSettings(cfg, profile.ID))
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			// Shutting down mid-startup is not a seeding failure. Seed is
			// idempotent, so the next boot finishes the job.
			slog.Info("capacity seeding interrupted by shutdown", "game", profile.ID)
			break
		}
		if err != nil {
			return d, fmt.Errorf("seed capacity for %s: %w", profile.ID, err)
		}
	}

	azOpts := []authz.Option{authz.WithAudit(st)}
	if reg := buildRoleRegistry(cfg); reg != nil {
		azOpts = append(azOpts, authz.WithRoleRegistry(reg))
	}
	d.Authz = authz.New(st, st, azOpts...)

	if d.Authz != nil && d.Authz.HasRegistry() {
		instOpts = append(instOpts,
			instances.WithInstanceRoleRegistrar(d.Authz.EnsureInstanceRole),
			instances.WithInstanceRoleRetirer(d.Authz.RetireInstanceRole),
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

	auth, err := buildAuth(ctx, cfg, st, d.Authz)
	if err != nil {
		return d, err
	}
	d.Auth = auth

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
	)
	if d.TS != nil {
		valheimInstOpts = append(valheimInstOpts,
			instances.WithVersionResolver(func(ctx context.Context, fullName string) (string, error) {
				ns, name, ok := strings.Cut(fullName, "-")
				if !ok {
					return "", fmt.Errorf("not a thunderstore full name: %s", fullName)
				}
				if hit, ok := d.TS.Get(ns + "/" + name); ok && hit.Version != "" {
					return hit.Version, nil
				}
				v, _, err := d.TS.LatestVersion(ctx, ns, name)
				switch {
				case errors.Is(err, thunderstore.ErrNotFound):
					return "", fmt.Errorf("no such mod on Thunderstore")
				case err != nil:
					return "", fmt.Errorf("could not reach Thunderstore: %w", err)
				}
				return v, nil
			}),
		)
	}
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
				if err != nil || inst == nil {
					if err == nil {
						err = fmt.Errorf("instance %d not found", num)
					}
					return nil, err
				}
				return d.K8s.ConfigMapData(ctx, inst.ConfigsCMName(domain.ValheimProfile))
			}),
			instances.WithModsReader(func(ctx context.Context, num int) ([]string, error) {
				inst, err := d.ValheimInstances.GetInstance(ctx, num)
				if err != nil || inst == nil {
					if err == nil {
						err = fmt.Errorf("instance %d not found", num)
					}
					return nil, err
				}
				cm, err := d.K8s.ConfigMapData(ctx, inst.ModsCMName(domain.ValheimProfile))
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
			instances.WithSourceReconciler(func(ctx context.Context, inst *domain.Instance) (bool, error) {
				if inst == nil || d.K8s == nil {
					return false, nil
				}
				depName := inst.DeploymentName(domain.ValheimProfile)
				v, found, err := d.K8s.DeploymentEnv(ctx, depName, "BEPINEX")
				if err != nil && inst.Number == 1 && cfg.ValheimDeployment != "" {
					v, found, err = d.K8s.DeploymentEnv(ctx, cfg.ValheimDeployment, "BEPINEX")
				}
				if err != nil || !found {
					return false, err
				}
				expected := domain.SourceModlist
				if strings.EqualFold(v, "false") {
					expected = domain.SourceVanilla
				}
				if inst.Source != expected {
					inst.Source = expected
					return true, nil
				}
				return false, nil
			}),
		)
	}
	valheimInstOpts = append(valheimInstOpts,
		instances.WithServerRefResolver(func(inst domain.Instance) ports.ServerRef {
			depName := inst.DeploymentName(domain.ValheimProfile)
			if inst.Number == 1 && cfg.ValheimDeployment != "" && d.K8s != nil {
				if _, _, err := d.K8s.DeploymentReplicas(context.Background(), depName); err != nil {
					return ports.ServerRef{Name: cfg.ValheimDeployment, Scope: cfg.ValheimNamespace}
				}
			}
			return ports.ServerRef{Name: depName, Scope: cfg.ValheimNamespace}
		}),
	)

	if d.Authz != nil && d.Authz.HasRegistry() {
		valheimInstOpts = append(valheimInstOpts,
			instances.WithInstanceRoleRegistrar(d.Authz.EnsureInstanceRole),
			instances.WithInstanceRoleRetirer(d.Authz.RetireInstanceRole),
		)
	}

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
		valheim.WithBepInExVersion(func(ctx context.Context) (string, error) {
			if d.TS == nil {
				return "", fmt.Errorf("thunderstore unconfigured")
			}
			if hit, ok := d.TS.Get("denikson-BepInExPack_Valheim"); ok && hit.Version != "" {
				return hit.Version, nil
			}
			v, _, err := d.TS.LatestVersion(ctx, "denikson", "BepInExPack_Valheim")
			return v, err
		}),
		valheim.WithBundleSource(func(ctx context.Context, inst domain.Instance) ([]string, map[string]string, error) {
			var entries []string
			configs := map[string]string{}
			if d.K8s == nil {
				return entries, configs, nil
			}

			// An Instance keeps its mods in its own ConfigMap; the bare names are
			// the pre-instance server and are all a legacy export has to go on.
			modsCM, configsCM := "valheim-mods", "valheim-mod-configs"
			if inst.Number > 0 && inst.Slug != "" {
				inst.GameID = domain.GameValheim
				modsCM, configsCM = inst.ModsCMName(domain.ValheimProfile), inst.ConfigsCMName(domain.ValheimProfile)
			}

			if data, err := d.K8s.ConfigMapData(ctx, modsCM); err == nil {
				entries = modpack.ResolveVersions(mods.Parse(data["mods.txt"]), func(fullName string) (string, bool) {
					if d.TS == nil {
						return "", false
					}
					if hit, ok := d.TS.Get(fullName); ok && hit.Version != "" {
						return hit.Version, true
					}
					ns, name, ok := strings.Cut(fullName, "-")
					if !ok {
						return "", false
					}
					v, _, err := d.TS.LatestVersion(ctx, ns, name)
					return v, err == nil && v != ""
				})
			} else {
				slog.Warn("valheim export: cannot read mod list", "configmap", modsCM, "err", err)
			}
			if cfgData, err := d.K8s.ConfigMapData(ctx, configsCM); err == nil {
				maps.Copy(configs, cfgData)
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
					loader := domain.LoaderNeoForge
					if inst.Minecraft != nil {
						loader = inst.Minecraft.Loader
						if inst.Minecraft.Pack != nil {
							pack = inst.Minecraft.Pack.Name
						}
					}
					return loader, pack
				}
			}
			return domain.LoaderNeoForge, ""
		}),
		minecraft.WithBundleBuilder(func(ctx context.Context, inst domain.Instance) (domain.Bundle, error) {
			var slugs []string
			cfgFiles := make(map[string]string)
			if d.MCK8s != nil {
				if data, err := d.MCK8s.ConfigMapData(ctx, inst.ModsCMName(domain.MinecraftProfile)); err == nil {
					if modsTxt, ok := data["mods.txt"]; ok {
						for line := range strings.SplitSeq(modsTxt, "\n") {
							line = strings.TrimSpace(line)
							if line != "" && !strings.HasPrefix(line, "#") {
								slugs = append(slugs, strings.TrimSuffix(line, "?"))
							}
						}
					}
				} else {
					slog.Warn("minecraft export: cannot read mods configmap from k8s", "configmap", inst.ModsCMName(domain.MinecraftProfile), "err", err)
				}
				if cfgData, err := d.MCK8s.ConfigMapData(ctx, inst.ConfigsCMName(domain.MinecraftProfile)); err == nil {
					cfgFiles = cfgData
				} else {
					slog.Warn("minecraft export: cannot read configs configmap from k8s", "configmap", inst.ConfigsCMName(domain.MinecraftProfile), "err", err)
				}
			}

			if len(slugs) == 0 && d.MCInstances != nil {
				if mods, err := d.MCInstances.GetInstalledMods(ctx, inst.Number); err == nil && len(mods) > 0 {
					slugs = mods
				} else if err != nil {
					slog.Warn("minecraft export: cannot read installed mods from instance manager", "number", inst.Number, "err", err)
				}
			}
			if len(cfgFiles) == 0 && d.MCInstances != nil {
				if names, err := d.MCInstances.ListConfigs(ctx, inst.Number); err == nil {
					for _, name := range names {
						if content, err := d.MCInstances.GetConfig(ctx, inst.Number, name); err == nil {
							cfgFiles[name] = content
						}
					}
				}
			}

			loader := string(domain.LoaderNeoForge)
			mcVer := "1.21.1"
			if inst.Minecraft != nil {
				if inst.Minecraft.Loader != "" {
					loader = string(inst.Minecraft.Loader)
				}
				if inst.Minecraft.MCVersion != "" {
					mcVer = inst.Minecraft.MCVersion
				}
			}

			// CurseForge mods are embedded rather than merely listed, so the
			// exported pack contains what the server actually runs.
			var cfExport []modpack.CurseForgeFile
			if d.CF.Ready() && d.MCInstances != nil {
				if entries, err := d.MCInstances.GetCurseForgeMods(ctx, inst.Number); err == nil && len(entries) > 0 {
					files, err := d.CF.ExportFiles(ctx, entries)
					if err != nil {
						slog.Warn("minecraft export: cannot resolve curseforge files", "number", inst.Number, "err", err)
					}
					for _, f := range files {
						cfExport = append(cfExport, modpack.CurseForgeFile{
							Slug: f.Slug, FileName: f.FileName, URL: f.URL, SHA1: f.SHA1, Size: f.Size,
						})
					}
				}
			}

			mrpackBytes, err := buildMrpack(ctx, d.MR, inst.Name, mcVer, loader, "", slugs, cfExport, cfgFiles)
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

	d.Games = games.NewRegistry()
	d.Games.Register(games.Entry{
		Profile: domain.MinecraftProfile,
		Engine:  d.MinecraftGame,
	})
	d.Games.Register(games.Entry{
		Profile: domain.ValheimProfile,
		Engine:  d.ValheimGame,
	})

	backfillValheimSource(ctx, &d, cfg)

	// BepInEx config editing needs three things that live in different places:
	// the Generated configs the publish sidecar leaves on the backups export,
	// the Override sets held in the live ConfigMap, and the git write path the
	// instance manager already owns. Without a backups directory there is
	// nothing published to read, and the Configs tab says so rather than
	// pretending the world has no settings.
	if d.ValheimInstances != nil && cfg.BackupsDir != "" {
		d.ValheimConfigs = appbepinex.New(
			appbepinex.WithSnapshot(func(slug string, num int) (*domain.ConfigSnapshot, error) {
				return infrabackups.ReadConfigSnapshot(cfg.BackupsDir, slug, num)
			}),
			appbepinex.WithFile(func(slug string, num int, name string) (string, string, error) {
				return infrabackups.ReadConfigFile(cfg.BackupsDir, slug, num, name)
			}),
			appbepinex.WithOverrides(func(ctx context.Context, num int) (map[string]string, error) {
				return d.ValheimInstances.ListConfigData(ctx, num)
			}),
			appbepinex.WithWriter(d.ValheimInstances),
		)
	}

	// Mods read their configuration once, at load, so a committed Override does
	// nothing until the pod restarts. Rolling immediately would disconnect
	// whoever is playing over a setting they never asked about, so the restart
	// waits for the world to empty - and occupancy comes from the server's own
	// status endpoint, per world, because the log-tailing route was wired to a
	// deployment name that does not exist and reported nothing at all.
	if d.ValheimInstances != nil {
		d.ValheimStatus = valheimstatus.New(cfg.ValheimNamespace)
		d.ValheimOccupancy = occupancy.New()
		watchValheimLogs(ctx, cfg, &d)

		opts := []restarts.Option{
			restarts.WithLookup(d.ValheimInstances.GetInstance),
			restarts.WithOccupancy(func(ctx context.Context, inst domain.Instance) (int, bool) {
				if n, ok := d.ValheimOccupancy.Players(inst.Number); ok {
					return n, true
				}
				return d.ValheimStatus.Players(ctx, inst.ServiceName(domain.ValheimProfile))
			}),
		}
		if d.ValheimRuntime != nil {
			ref := func(inst domain.Instance) ports.ServerRef {
				return ports.ServerRef{Name: inst.DeploymentName(domain.ValheimProfile), Scope: cfg.ValheimNamespace}
			}
			opts = append(opts,
				restarts.WithRestarter(func(ctx context.Context, inst domain.Instance) error {
					return d.ValheimRuntime.Restart(ctx, ref(inst))
				}),
				restarts.WithStartedAt(func(ctx context.Context, inst domain.Instance) (time.Time, bool) {
					st, err := d.ValheimRuntime.Status(ctx, ref(inst))
					if err != nil || st.StartedAt.IsZero() {
						return time.Time{}, false
					}
					return st.StartedAt, true
				}),
			)
		}
		if st != nil {
			opts = append(opts, restarts.WithEvent(func(kind, detail string) {
				_ = st.RecordEvent(kind, detail)
			}))
		}
		d.ValheimRestarts = restarts.New(opts...)
		d.ValheimRestarts.Run(ctx)
	}

	if d.ValheimInstances != nil && d.TS != nil {
		d.ModUpdates = modupdates.New(d.ValheimInstances, d.TS, modupdates.WithRestorePoints(st))
		d.ModUpdates.Start(ctx)
	}
	if d.MCInstances != nil && d.MR != nil {
		d.MCModUpdates = modupdates.New(d.MCInstances, d.MR,
			modupdates.WithGameID(domain.GameMinecraft),
			modupdates.WithRestorePoints(st),
		)
		d.MCModUpdates.Start(ctx)
	}

	d.Requests = requests.New(st,
		requests.WithAudit(st),
		requests.WithCreationLimit(d.Capacity.FreeCreations),
		requests.WithList(func(ctx context.Context) ([]domain.Instance, error) {
			var out []domain.Instance
			for _, m := range []*instances.InstanceManager{d.ValheimInstances, d.MCInstances} {
				if m == nil {
					continue
				}
				got, err := m.ListInstances(ctx)
				if err != nil {
					return nil, err
				}
				out = append(out, got...)
			}
			return out, nil
		}),
		requests.WithCreate(func(ctx context.Context, game domain.GameID, inst domain.Instance, mods domain.ModList, actor string) (*domain.Instance, error) {
			var m *instances.InstanceManager
			switch game {
			case domain.GameMinecraft:
				m = d.MCInstances
			case domain.GameValheim:
				m = d.ValheimInstances
			default:
				return nil, fmt.Errorf("no instance manager for %s", game)
			}
			if m == nil {
				return nil, fmt.Errorf("no instance manager for %s", game)
			}
			return m.CreateInstance(ctx, inst, mods, actor)
		}),
		requests.WithGrant(func(ctx context.Context, subject string, game domain.GameID, number int, actor string) error {
			if d.Authz == nil {
				return nil
			}
			return d.Authz.Grant(ctx, subject, game, number, actor)
		}),
	)

	health.New(st, healthSources(&d)).Start(ctx)
	startEventPruner(ctx, st)

	return d, nil
}

// healthSources lists the game managers the health watcher scans. A manager with
// no runtime (dev, or a game not configured) is left out rather than polled.
func healthSources(d *Deps) []health.Source {
	var out []health.Source
	for _, m := range []*instances.InstanceManager{d.MCInstances, d.ValheimInstances} {
		if m != nil {
			out = append(out, m)
		}
	}
	return out
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

	if mcK8s, err := newK8sClient(cfg.MinecraftNamespace, cfg.MinecraftDeployment); err != nil {
		slog.Warn("minecraft k8s client unavailable (dev?)", "err", err)
	} else {
		mcK8s.SetNodeSelector(cfg.GameNodeSelector)
		d.MCK8s = mcK8s
	}
	return k8sruntime.New(d.MCK8s)
}

// defaultGameSettings supplies the starting budget and ceiling for a game the
// first time agrelha sees it. They come from the environment so an existing
// deployment keeps the limits it already had; afterwards the stored values win
// and these are never consulted again.
func defaultGameSettings(cfg *config.Config, gameID domain.GameID) domain.GameSettings {
	g := domain.GameSettings{GameID: gameID}
	switch gameID {
	case domain.GameValheim:
		g.TotalBudgetGiB = cfg.ValheimTotalBudgetGiB
		g.MaxInstances = cfg.ValheimMaxInstances
		g.MaxRunning = cfg.ValheimMaxRunning
	case domain.GameMinecraft:
		g.TotalBudgetGiB = cfg.MCTotalBudgetGiB
		g.MaxInstances = cfg.MCMaxInstances
		g.MaxRunning = cfg.MCMaxRunning
	}
	for _, t := range domain.DefaultTiersFor(gameID) {
		if t.Resources.MemLimitGiB > g.Ceiling.MemLimitGiB {
			g.Ceiling.MemLimitGiB = t.Resources.MemLimitGiB
		}
	}
	return g
}

func buildRoleRegistry(cfg *config.Config) ports.RoleRegistry {
	if cfg.ZitadelProjectID == "" || cfg.ZitadelServiceKey == "" {
		slog.Info("zitadel role registry disabled: ZITADEL_PROJECT_ID or ZITADEL_SERVICE_KEY unset")
		return nil
	}
	c, err := zitadelroles.New(zitadelroles.Config{
		Issuer:     cfg.OIDCIssuer,
		APIBaseURL: cfg.ZitadelAPIURL,
		ProjectID:  cfg.ZitadelProjectID,
		ServiceKey: []byte(cfg.ZitadelServiceKey),
	})
	if err != nil {
		slog.Error("zitadel role registry unavailable", "err", err)
		return nil
	}
	return c
}

func buildAuth(ctx context.Context, cfg *config.Config, st *store.Store, az *authz.Service) (ports.Auth, error) {
	oidcCfg := oidc.Config{
		Issuer:        cfg.OIDCIssuer,
		ClientID:      cfg.OIDCClientID,
		ClientSecret:  cfg.OIDCClientSecret,
		RedirectURL:   cfg.OIDCRedirectURL,
		PostLogoutURL: cfg.OIDCPostLogoutURL,
		ProjectID:     cfg.ZitadelProjectID,
	}
	opts := []oidc.Option{oidc.WithSessions(st)}
	if az != nil {
		opts = append(opts, oidc.WithOnSignIn(az.RecordSignIn))
	}

	if cfg.OIDCIssuer != "" {
		a, err := oidc.New(ctx, oidcCfg, opts...)
		if err != nil {
			return nil, fmt.Errorf("oidc discovery failed for %q: %w", cfg.OIDCIssuer, err)
		}
		return a, nil
	}

	if cfg.AuthMode == "dev" {
		slog.Warn("AUTH_MODE=dev: passwordless dev authenticator, never use this outside local development")
		return oidc.NewDev(oidcCfg, opts...), nil
	}

	users, err := st.ListUsers(ctx)
	if err == nil && len(users) > 0 {
		slog.Info("oidc unset: local user accounts found, running with local authenticator", "users", len(users))
		return local.New(st, cfg.OIDCClientSecret), nil
	}

	return nil, errors.New("no authentication configured: set OIDC_ISSUER, create a local user, or set AUTH_MODE=dev")
}

// backfillValheimSource reconciles Valheim Worlds' Source, derived from
// what their deployment actually runs (e.g. BEPINEX="true" vs "false").
// Source was not stored before Vanilla became a real choice, and manual GitOps
// changes to BEPINEX are reflected into Agrelha's store on startup and whenever
// instances are read.
func backfillValheimSource(ctx context.Context, d *Deps, cfgs ...*config.Config) {
	if d.ValheimInstances == nil || d.K8s == nil || d.Store == nil {
		return
	}
	var cfg *config.Config
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	records, err := d.Store.ListValheimInstances()
	if err != nil {
		slog.Warn("valheim source backfill: cannot query instances", "err", err)
		return
	}
	if len(records) == 0 {
		return
	}

	for _, rec := range records {
		inst, err := d.ValheimInstances.GetInstance(ctx, rec.Number)
		if err != nil || inst == nil {
			slog.Warn("valheim source backfill: cannot load instance", "instance", rec.Number, "err", err)
			continue
		}
		expectedSource := inst.Source
		depName := inst.DeploymentName(domain.ValheimProfile)
		v, found, err := d.K8s.DeploymentEnv(ctx, depName, "BEPINEX")
		if err != nil && rec.Number == 1 && cfg != nil && cfg.ValheimDeployment != "" {
			v, found, err = d.K8s.DeploymentEnv(ctx, cfg.ValheimDeployment, "BEPINEX")
		}
		if err != nil {
			slog.Warn("valheim source backfill: cannot read deployment", "instance", inst.Number, "err", err)
			continue
		} else if found {
			if strings.EqualFold(v, "false") {
				expectedSource = domain.SourceVanilla
			} else if strings.EqualFold(v, "true") {
				expectedSource = domain.SourceModlist
			}
		} else if rec.Source == "" {
			expectedSource = domain.SourceModlist
		}
		if inst.Source != expectedSource || rec.Source == "" {
			inst.Source = expectedSource
			if err := d.ValheimInstances.SaveInstance(*inst); err != nil {
				slog.Warn("valheim source backfill: cannot save", "instance", inst.Number, "err", err)
				continue
			}
			slog.Info("valheim source backfilled from the running deployment", "instance", inst.Number, "name", inst.Name, "source", expectedSource)
		}
	}
}

func watchValheimLogs(ctx context.Context, cfg *config.Config, d *Deps) {
	if d.ValheimInstances == nil || d.ValheimOccupancy == nil {
		return
	}
	insts, err := d.ValheimInstances.ListInstances(ctx)
	if err != nil {
		slog.Warn("occupancy: cannot list valheim instances", "err", err)
		return
	}
	for _, inst := range insts {
		c, err := newK8sClient(cfg.ValheimNamespace, inst.DeploymentName(domain.ValheimProfile))
		if err != nil {
			slog.Warn("occupancy: no log stream for instance", "instance", inst.Number, "err", err)
			continue
		}
		d.ValheimOccupancy.Watch(ctx, inst.Number, c)
	}
}

const (
	eventRetention = 90 * 24 * time.Hour
	prunerInterval = 24 * time.Hour
)

func startEventPruner(ctx context.Context, st *store.Store) {
	if st == nil {
		return
	}
	prune := func() {
		n, err := st.PruneEvents(time.Now().Add(-eventRetention))
		if err != nil {
			slog.Warn("event prune failed", "err", err)
			return
		}
		if n > 0 {
			slog.Info("pruned old events", "rows", n, "older_than", eventRetention.String())
		}
	}
	prune()
	go func() {
		t := time.NewTicker(prunerInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				prune()
			}
		}
	}()
}
