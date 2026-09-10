package wiring

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	appbackups "agrelha/internal/app/backups"
	mccontent "agrelha/internal/app/content"
	"agrelha/internal/app/instances"
	"agrelha/internal/app/mods"
	"agrelha/internal/domain"
	infrabackups "agrelha/internal/infra/backups"
	"agrelha/internal/infra/content/modpackindex"
	k8sruntime "agrelha/internal/infra/runtime/k8s"
	"agrelha/internal/infra/store"
	"agrelha/internal/platform/config"
	"agrelha/internal/ports"
	"agrelha/internal/web"
	"agrelha/internal/web/handlers/access"
	backupshttp "agrelha/internal/web/handlers/backups"
	consolehttp "agrelha/internal/web/handlers/console"
	contenthttp "agrelha/internal/web/handlers/content"
	dashboardhttp "agrelha/internal/web/handlers/dashboard"
	instanceshttp "agrelha/internal/web/handlers/instances"
	wizardhttp "agrelha/internal/web/handlers/wizard"
	"agrelha/internal/web/pages"
)

// BuildServer constructs the HTTP application with all handlers and background reconciliation loops.
func BuildServer(ctx context.Context, cfg *config.Config, d Deps) *fiber.App {
	applyMCAfterSync := makeApplyMinecraftAfterSync(cfg, d)
	if d.MCInstances != nil {
		d.MCInstances.ApplyOptions(instances.WithAfterSyncHook(applyMCAfterSync))
	}

	if d.ValheimInstances != nil && d.Store != nil {
		defaultLBIP := ""
		serverName := "Valheim"
		if cfg != nil {
			defaultLBIP = cfg.ValheimLBBaseIP
			if cfg.ValheimDeployment != "" {
				serverName = cfg.ValheimDeployment
			}
		}
		if _, err := instances.AdoptLegacyValheim(
			ctx,
			store.NewValheimInstanceRepo(d.Store),
			d.ValheimRuntime,
			d.ValheimRef,
			defaultLBIP,
			serverName,
		); err != nil {
			slog.Warn("adopt legacy valheim instance failed", "err", err)
		}
	}

	applyValheimAfterSync := makeApplyValheimAfterSync(cfg, d)
	if d.ValheimInstances != nil {
		d.ValheimInstances.ApplyOptions(instances.WithAfterSyncHook(applyValheimAfterSync))
	}

	startMinecraftScheduler(ctx, cfg, d)
	startValheimScheduler(ctx, cfg, d)

	applyAfterSync := makeApplyAfterSync(d)

	accessH := buildAccessHandler(d, applyAfterSync)
	backupsH := buildBackupsHandler(cfg, d)
	consoleH := buildConsoleHandler(d)
	contentH := buildContentHandler(cfg, d, applyAfterSync)
	instancesH := buildInstancesHandler(cfg, d, applyMCAfterSync)
	wizardH := buildWizardHandler(d)
	dashboardH := buildDashboardHandler(cfg, d, contentH, instancesH, backupsH)

	return web.New(web.ServerConfig{
		Auth:      d.Auth,
		Access:    accessH,
		Backups:   backupsH,
		Console:   consoleH,
		Content:   contentH,
		Dashboard: dashboardH,
		Instances: instancesH,
		Wizard:    wizardH,
	})
}

func actor(c *fiber.Ctx) string {
	if v, ok := c.Locals("actor").(string); ok && v != "" {
		return v
	}
	return "local"
}

func makeApplyAfterSync(d Deps) func(string, string, func(string) bool) {
	return func(cmName, key string, want func(string) bool) {
		if d.K8s == nil {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			t := time.NewTicker(10 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					slog.Warn("applyAfterSync: timed out waiting for ArgoCD sync", "configmap", cmName)
					return
				case <-t.C:
					data, err := d.K8s.ConfigMapData(ctx, cmName)
					if err != nil {
						continue
					}
					if want(data[key]) {
						var restartErr error
						if d.ValheimRuntime != nil {
							restartErr = d.ValheimRuntime.Restart(ctx, d.ValheimRef)
						} else {
							restartErr = d.K8s.Restart(ctx)
						}
						if restartErr != nil {
							slog.Error("applyAfterSync: restart failed", "configmap", cmName, "err", restartErr)
							return
						}
						slog.Info("applyAfterSync: change landed, rolled valheim", "configmap", cmName)
						if d.Store != nil {
							_ = d.Store.RecordEvent("auto-restart", cmName)
						}
						return
					}
				}
			}
		}()
	}
}

func makeApplyMinecraftAfterSync(cfg *config.Config, d Deps) func(string, string, string, func(string) bool) {
	return func(cmName, depName, key string, want func(string) bool) {
		if d.MCK8s == nil {
			return
		}
		if depName == "" && cfg != nil {
			depName = cfg.MinecraftDeployment
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			t := time.NewTicker(10 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					slog.Warn("applyMinecraftAfterSync: timed out waiting for ArgoCD sync", "configmap", cmName)
					return
				case <-t.C:
					data, err := d.MCK8s.ConfigMapData(ctx, cmName)
					if err != nil {
						continue
					}
					if want(data[key]) {
						var restartErr error
						if d.MCRuntime != nil {
							scope := ""
							if cfg != nil {
								scope = cfg.MinecraftNamespace
							}
							restartErr = d.MCRuntime.Restart(ctx, ports.ServerRef{Name: depName, Scope: scope})
						} else {
							restartErr = d.MCK8s.RestartDeployment(ctx, depName)
						}
						if restartErr != nil {
							slog.Error("applyMinecraftAfterSync: restart failed", "configmap", cmName, "dep", depName, "err", restartErr)
							return
						}
						slog.Info("applyMinecraftAfterSync: change landed, rolled minecraft deployment", "configmap", cmName, "dep", depName)
						if d.Store != nil {
							_ = d.Store.RecordEvent("mc-auto-restart", cmName)
						}
						return
					}
				}
			}
		}()
	}
}

func startMinecraftScheduler(ctx context.Context, cfg *config.Config, d Deps) {
	var opts []appbackups.Option
	if cfg != nil {
		opts = append(opts, appbackups.WithNamespace(cfg.MinecraftNamespace))
		if cfg.BackupsDir != "" {
			opts = append(opts, appbackups.WithPruner(func(slug string, num, keep int) error {
				return infrabackups.PruneBackups(cfg.BackupsDir, slug, num, keep)
			}))
		}
	}
	if d.Store != nil {
		opts = append(opts,
			appbackups.WithAudit(d.Store),
			appbackups.WithEvent(d.Store),
		)
	}
	if d.MCInstances != nil {
		opts = append(opts, appbackups.WithCommandExecutor(func(ctx context.Context, inst domain.Instance, cmd string) error {
			_, err := d.MCInstances.ExecuteCommand(ctx, inst.Number, cmd)
			return err
		}))
	} else if d.MCRconPool != nil && cfg != nil {
		opts = append(opts, appbackups.WithCommandExecutor(func(ctx context.Context, inst domain.Instance, cmd string) error {
			addr := fmt.Sprintf("%s.%s.svc.cluster.local:25575", inst.ServiceName(), cfg.MinecraftNamespace)
			_, err := d.MCRconPool.ClientFor(addr).Execute(cmd)
			return err
		}))
	}
	sched := appbackups.New(d.MCInstances, d.MCK8s, opts...)
	sched.Start(ctx)
}

func makeApplyValheimAfterSync(cfg *config.Config, d Deps) func(string, string, string, func(string) bool) {
	return func(cmName, depName, key string, want func(string) bool) {
		if d.K8s == nil {
			return
		}
		if depName == "" && cfg != nil {
			depName = cfg.ValheimDeployment
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			t := time.NewTicker(10 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					slog.Warn("applyValheimAfterSync: timed out waiting for ArgoCD sync", "configmap", cmName)
					return
				case <-t.C:
					data, err := d.K8s.ConfigMapData(ctx, cmName)
					if err != nil {
						continue
					}
					if want(data[key]) {
						var restartErr error
						if d.ValheimRuntime != nil {
							scope := ""
							if cfg != nil {
								scope = cfg.ValheimNamespace
							}
							restartErr = d.ValheimRuntime.Restart(ctx, ports.ServerRef{Name: depName, Scope: scope})
						} else {
							restartErr = d.K8s.RestartDeployment(ctx, depName)
						}
						if restartErr != nil {
							slog.Error("applyValheimAfterSync: restart failed", "configmap", cmName, "dep", depName, "err", restartErr)
							return
						}
						slog.Info("applyValheimAfterSync: change landed, rolled valheim deployment", "configmap", cmName, "dep", depName)
						if d.Store != nil {
							_ = d.Store.RecordEvent("valheim-auto-restart", cmName)
						}
						return
					}
				}
			}
		}()
	}
}

func startValheimScheduler(ctx context.Context, cfg *config.Config, d Deps) {
	if d.ValheimInstances == nil || d.K8s == nil {
		return
	}
	var opts []appbackups.Option
	if cfg != nil {
		opts = append(opts,
			appbackups.WithNamespace(cfg.ValheimNamespace),
			appbackups.WithBackupsPVC("valheim-backups"),
		)
		if cfg.BackupsDir != "" {
			opts = append(opts, appbackups.WithPruner(func(slug string, num, keep int) error {
				return infrabackups.PruneBackups(cfg.BackupsDir, slug, num, keep)
			}))
		}
	}
	if d.Store != nil {
		opts = append(opts,
			appbackups.WithAudit(d.Store),
			appbackups.WithEvent(d.Store),
		)
	}
	sched := appbackups.New(d.ValheimInstances, d.K8s, opts...)
	sched.Start(ctx)
}

func buildAccessHandler(d Deps, applyAfterSync func(string, string, func(string) bool)) *access.Handler {
	return access.New(access.Config{
		Admins:         d.Admins,
		MCAccess:       d.MCAccess,
		History:        d.Store,
		Players:        d.Store,
		StateStore:     d.StateStore,
		Actor:          actor,
		ApplyAfterSync: applyAfterSync,
	})
}

func buildBackupsHandler(cfg *config.Config, d Deps) *backupshttp.Handler {
	backupsDir := ""
	if cfg != nil {
		backupsDir = cfg.BackupsDir
	}
	if d.MCInstances != nil {
		var opts []instances.Option
		if d.MCK8s != nil {
			opts = append(opts, instances.WithJobRunner(d.MCK8s))
		}
		if backupsDir != "" {
			opts = append(opts, instances.WithBackupsDir(backupsDir))
		}
		if len(opts) > 0 {
			d.MCInstances.ApplyOptions(opts...)
		}
	}
	return backupshttp.New(backupshttp.Config{
		BackupsDir:  backupsDir,
		MCInstances: d.MCInstances,
		Actor:       actor,
	})
}

func buildConsoleHandler(d Deps) *consolehttp.Handler {
	vrt := d.ValheimRuntime
	if vrt == nil && d.K8s != nil {
		vrt = k8sruntime.New(d.K8s)
	}
	mcrt := d.MCRuntime
	if mcrt == nil && d.MCK8s != nil {
		mcrt = k8sruntime.New(d.MCK8s)
	}
	return consolehttp.New(consolehttp.Config{
		ValheimRuntime: vrt,
		ValheimRef:     d.ValheimRef,
		MCRuntime:      mcrt,
		MCRef:          d.MCRef,
		MCInstances:    d.MCInstances,
		Audit:          d.Store,
		Event:          d.Store,
		Auth:           d.Auth,
		Actor:          actor,
	})
}

func buildContentHandler(cfg *config.Config, d Deps, applyAfterSync func(string, string, func(string) bool)) *contenthttp.Handler {
	var modConfigsPath string
	if cfg != nil {
		modConfigsPath = cfg.ModConfigsPath
	}
	var cat ports.PackageCatalog
	if d.TS != nil {
		cat = d.TS
	}
	var audit ports.AuditRecorder
	var readme ports.ReadmeCache
	if d.Store != nil {
		audit = d.Store
		readme = d.Store
	}
	return contenthttp.New(contenthttp.Config{
		ModConfigsPath: modConfigsPath,
		StateStore:     d.StateStore,
		Audit:          audit,
		ReadmeCache:    readme,
		ValheimGame:    d.ValheimGame,
		Mods:           d.Mods,
		TS:             cat,
		InstalledMods: func(ctx context.Context) ([]string, error) {
			if d.K8s != nil {
				data, err := d.K8s.ConfigMapData(ctx, "valheim-mods")
				if err != nil {
					return nil, err
				}
				return mods.Parse(data["mods.txt"]), nil
			}
			return nil, nil
		},
		ConfigData: func(ctx context.Context) (map[string]string, error) {
			if d.K8s != nil {
				return d.K8s.ConfigMapData(ctx, "valheim-mod-configs")
			}
			return nil, nil
		},
		Actor:          actor,
		ApplyAfterSync: applyAfterSync,
	})
}

func buildInstancesHandler(cfg *config.Config, d Deps, applyMCAfterSync func(string, string, string, func(string) bool)) *instanceshttp.Handler {
	if d.MCInstances != nil {
		var opts []instances.Option
		if cfg != nil && cfg.BackupsDir != "" {
			opts = append(opts, instances.WithBackupsDir(cfg.BackupsDir))
		}
		if d.MCK8s != nil {
			opts = append(opts,
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
		if len(opts) > 0 {
			d.MCInstances.ApplyOptions(opts...)
		}
	}
	return instanceshttp.New(instanceshttp.Config{
		MCInstances:             d.MCInstances,
		MinecraftGame:           d.MinecraftGame,
		Actor:                   actor,
		ApplyMinecraftAfterSync: applyMCAfterSync,
	})
}

func buildWizardHandler(d Deps) *wizardhttp.Handler {
	var releases func(ctx context.Context, limit int) []string
	if d.MCV != nil {
		releases = d.MCV.Releases
	}

	var searchModpacks func(ctx context.Context, query string) ([]wizardhttp.ModpackHit, error)
	if d.MPI != nil {
		searchModpacks = func(ctx context.Context, query string) ([]wizardhttp.ModpackHit, error) {
			res, err := d.MPI.SearchModpacks(ctx, query, "", 1)
			if err != nil {
				return nil, err
			}
			hits := make([]wizardhttp.ModpackHit, 0, len(res.Data))
			for _, p := range res.Data {
				packRefURL := ""
				if p.Links != nil && p.Links["curseforge"] != "" {
					packRefURL = p.Links["curseforge"]
				} else if strings.Contains(p.URL, "curseforge.com") {
					packRefURL = p.URL
				} else if p.URL != "" {
					packRefURL = p.URL
				} else {
					packRefURL = p.PageURL
				}
				hits = append(hits, wizardhttp.ModpackHit{
					ID:            p.ID,
					Name:          p.Name,
					Summary:       p.Summary,
					ThumbnailURL:  p.ThumbnailURL,
					DownloadCount: p.DownloadCount,
					RefURL:        packRefURL,
				})
			}
			return hits, nil
		}
	}

	var resolvePackRef func(ctx context.Context, packRef string) string
	if d.MPI != nil {
		resolvePackRef = func(ctx context.Context, packRef string) string {
			if !strings.Contains(packRef, "modpackindex.com/modpack/") {
				return packRef
			}
			parts := strings.Split(packRef, "/")
			for i, part := range parts {
				if part == "modpack" && i+1 < len(parts) {
					if id, err := strconv.Atoi(parts[i+1]); err == nil && id > 0 {
						if detail, err := d.MPI.GetModpack(ctx, id); err == nil && detail != nil {
							if cfURL := detail.Links["curseforge"]; cfURL != "" {
								return cfURL
							} else if detail.URL != "" && strings.Contains(detail.URL, "curseforge.com") {
								return detail.URL
							}
						}
					}
					break
				}
			}
			return packRef
		}
	}

	var verifyPackLoader func(ctx context.Context, packID int, packName, requestedLoader string) string
	if d.MPI != nil {
		verifyPackLoader = func(ctx context.Context, packID int, packName, requestedLoader string) string {
			mods, err := d.MPI.GetModpackMods(ctx, packID)
			if err != nil || len(mods) == 0 {
				return requestedLoader
			}
			best := modpackindex.BestLoader(mods)
			if best != "" && best != requestedLoader {
				fit := modpackindex.AnalyzeLoader(mods, requestedLoader)
				slog.Warn("wizard: loader corrected from pack contents",
					"pack", packName, "requested", requestedLoader, "derived", best,
					"would_not_load", len(fit.Blocking))
				return best
			}
			return requestedLoader
		}
	}

	var searchMods func(ctx context.Context, query, mcVersion string) ([]wizardhttp.ModHit, error)
	if d.MR != nil {
		searchMods = func(ctx context.Context, query, mcVersion string) ([]wizardhttp.ModHit, error) {
			res, err := d.MR.Search(ctx, query, mcVersion, "", 20, 0)
			if err != nil {
				return nil, err
			}
			hits := make([]wizardhttp.ModHit, 0, len(res.Hits))
			for _, h := range res.Hits {
				hits = append(hits, wizardhttp.ModHit{
					Slug:        h.Slug,
					Title:       h.Title,
					Description: h.Description,
					IconURL:     h.IconURL,
				})
			}
			return hits, nil
		}
	}

	var checkCartCompat func(ctx context.Context, slugs []string, mcVersion string) mccontent.CartCompatibility
	if d.MR != nil {
		checkCartCompat = func(ctx context.Context, slugs []string, mcVersion string) mccontent.CartCompatibility {
			return mccontent.CheckCartCompatibility(ctx, d.MR, slugs, mcVersion)
		}
	}

	return wizardhttp.New(wizardhttp.Config{
		MCInstances:      d.MCInstances,
		Actor:            actor,
		VersionReleases:  releases,
		SearchModpacks:   searchModpacks,
		ResolvePackRef:   resolvePackRef,
		VerifyPackLoader: verifyPackLoader,
		SearchMods:       searchMods,
		CheckCartCompat:  checkCartCompat,
	})
}

func buildDashboardHandler(cfg *config.Config, d Deps, contentH *contenthttp.Handler, instancesH *instanceshttp.Handler, backupsH *backupshttp.Handler) *dashboardhttp.Handler {
	var grafanaURL, valheimAddr, nodeName string
	if cfg != nil {
		grafanaURL = cfg.GrafanaDashboardURL
		valheimAddr = cfg.ValheimAddress
		nodeName = cfg.GameNodeName
	}
	bkInfo := func() (dashboardhttp.BackupSummary, bool) {
		if backupsH == nil {
			return dashboardhttp.BackupSummary{}, false
		}
		bi, ok := backupsH.BackupInfo()
		return dashboardhttp.BackupSummary{
			Count:      bi.Count,
			TotalSize:  bi.TotalSize,
			LatestSize: bi.LatestSize,
			LatestAt:   bi.LatestAt,
		}, ok
	}
	var modUpdates func(context.Context) []pages.ModUpdate
	var pendingActive func(context.Context) bool
	if contentH != nil {
		modUpdates = contentH.ModUpdates
		pendingActive = contentH.PendingActive
	}
	var instStats func(context.Context, []domain.Instance) map[int]dashboardhttp.InstanceStat
	if instancesH != nil {
		instStats = func(ctx context.Context, insts []domain.Instance) map[int]dashboardhttp.InstanceStat {
			raw := instancesH.InstanceStats(ctx, insts)
			res := make(map[int]dashboardhttp.InstanceStat, len(raw))
			for k, v := range raw {
				res[k] = dashboardhttp.InstanceStat{
					Players:      v.Players,
					PlayersKnown: v.PlayersKnown,
					Uptime:       v.Uptime,
				}
			}
			return res
		}
	}
	return dashboardhttp.New(dashboardhttp.Config{
		GrafanaDashboardURL: grafanaURL,
		ValheimAddress:      valheimAddr,
		GameNodeName:        nodeName,
		MCInstances:         d.MCInstances,
		ValheimGame:         d.ValheimGame,
		MinecraftGame:       d.MinecraftGame,
		Auth:                d.Auth,
		Actor:               actor,
		BackupInfo:          bkInfo,
		ModUpdates:          modUpdates,
		PendingActive:       pendingActive,
		InstanceStats:       instStats,
	})
}
