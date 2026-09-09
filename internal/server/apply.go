package server

import (
	"context"
	"log/slog"
	"time"

	"agrelha/internal/ports"
)

// applyAfterSync waits (in the background) for ArgoCD to reconcile a committed
// ConfigMap change into the live cluster, then rolls the valheim pod so the
// change takes effect (mods re-install on start; ADMINLIST_IDS renders on start).
// `want` inspects the live data[key] and reports whether the change has landed.
// Gives up after 5 minutes.
func (s *FiberServer) applyAfterSync(cmName, key string, want func(string) bool) {
	if s.k8s == nil {
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
				data, err := s.k8s.ConfigMapData(ctx, cmName)
				if err != nil {
					continue
				}
				if want(data[key]) {
					var restartErr error
					if s.valheimRuntime != nil {
						restartErr = s.valheimRuntime.Restart(ctx, s.valheimRef)
					} else {
						restartErr = s.k8s.Restart(ctx)
					}
					if restartErr != nil {
						slog.Error("applyAfterSync: restart failed", "configmap", cmName, "err", restartErr)
						return
					}
					slog.Info("applyAfterSync: change landed, rolled valheim", "configmap", cmName)
					_ = s.store.RecordEvent("auto-restart", cmName)
					return
				}
			}
		}
	}()
}

// applyMinecraftAfterSync waits for ArgoCD to reconcile a committed ConfigMap
// in the minecraft namespace, then rolls the specified minecraft deployment.
func (s *FiberServer) applyMinecraftAfterSync(cmName, depName, key string, want func(string) bool) {
	if s.mck8s == nil {
		return
	}
	if depName == "" {
		depName = s.cfg.MinecraftDeployment
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
				data, err := s.mck8s.ConfigMapData(ctx, cmName)
				if err != nil {
					continue
				}
				if want(data[key]) {
					var restartErr error
					if s.mcRuntime != nil {
						scope := ""
						if s.cfg != nil {
							scope = s.cfg.MinecraftNamespace
						}
						restartErr = s.mcRuntime.Restart(ctx, ports.ServerRef{Name: depName, Scope: scope})
					} else {
						restartErr = s.mck8s.RestartDeployment(ctx, depName)
					}
					if restartErr != nil {
						slog.Error("applyMinecraftAfterSync: restart failed", "configmap", cmName, "dep", depName, "err", restartErr)
						return
					}
					slog.Info("applyMinecraftAfterSync: change landed, rolled minecraft deployment", "configmap", cmName, "dep", depName)
					_ = s.store.RecordEvent("mc-auto-restart", cmName)
					return
				}
			}
		}
	}()
}
