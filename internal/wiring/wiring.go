package wiring

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	mcaccess "agrelha/internal/app/access"
	"agrelha/internal/app/admins"
	"agrelha/internal/app/ingest"
	"agrelha/internal/app/instances"
	"agrelha/internal/app/mods"
	"agrelha/internal/infra/auth/local"
	"agrelha/internal/infra/auth/oidc"
	"agrelha/internal/infra/content/mcversions"
	"agrelha/internal/infra/content/modpackindex"
	"agrelha/internal/infra/content/modrinth"
	"agrelha/internal/infra/content/thunderstore"
	"agrelha/internal/infra/gitops"
	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/manifests"
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
	"agrelha/internal/server"
	contenthttp "agrelha/internal/web/handlers/content"
)

// Build is the composition root: it is the only place that decides which
// adapter satisfies which port. Everything it returns is already constructed,
// so server.New performs no I/O and can be handed fakes in a test.
func Build(ctx context.Context, cfg *config.Config) (server.Deps, error) {
	var d server.Deps

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
		d.Admins = admins.New(d.StateStore, cfg.AdminsPath)
		d.MCMods = mcaccess.NewModManager(d.StateStore, cfg.MinecraftModsPath)

		var console ports.Console
		if d.MCRcon != nil {
			console = d.MCRcon
		}
		d.MCAccess = mcaccess.NewAccessManager(d.StateStore, cfg.MinecraftAccessPath, console)
	}

	rt := buildRuntime(cfg, &d)

	if c, err := k8s.New(cfg.ValheimNamespace, cfg.ValheimDeployment); err != nil {
		slog.Warn("k8s client unavailable (dev?)", "err", err)
	} else {
		d.K8s = c
		go ingest.Run(ctx, c, st)
	}

	d.MCInstances = instances.NewInstanceManager(
		store.NewInstanceRepo(st), d.StateStore, rt,
		cfg.MCTotalBudgetGiB, cfg.MCMaxInstances, cfg.MCMaxRunning,
		cfg.MCInstancesPath,
		cfg.MCLBBaseIP,
		manifests.New(cfg.GameNodeSelector, cfg.MinecraftNamespace),
		cfg.MinecraftNamespace,
	)

	d.Auth = buildAuth(ctx, cfg, st)

	return d, nil
}

func buildThunderstore(ctx context.Context, cfg *config.Config, st *store.Store) *thunderstore.Client {
	ts := thunderstore.New(cfg.ThunderstoreAPI)

	if rows, fetchedAt, err := st.LoadModIndex(); err != nil {
		slog.Warn("mod index load failed", "err", err)
	} else if len(rows) > 0 {
		ts.Preload(contenthttp.RowsToResults(rows), fetchedAt)
		slog.Info("thunderstore index preloaded from cache", "packages", len(rows))
	}

	ts.OnRefresh = func(idx []thunderstore.SearchResult) {
		if err := st.SaveModIndex(contenthttp.ResultsToRows(idx), time.Now()); err != nil {
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

func buildRuntime(cfg *config.Config, d *server.Deps) ports.Runtime {
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
	if cfg.OIDCIssuer != "" {
		a, err := oidc.New(ctx, cfg)
		if err != nil {
			slog.Warn("oidc unavailable, falling back to local dev auth", "err", err)
			return oidc.NewDev(cfg)
		}
		return a
	}

	users, err := st.ListUsers(ctx)
	if err == nil && len(users) > 0 {
		slog.Info("oidc unset: local user accounts found, running with local authenticator", "users", len(users))
		return local.New(st, cfg.OIDCClientSecret)
	}

	slog.Info("oidc unset: running with local dev authenticator (click 'Admin Sign In' to authenticate)")
	return oidc.NewDev(cfg)
}
