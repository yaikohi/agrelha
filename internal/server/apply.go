package server

import (
	"context"
	"log/slog"
	"time"
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
					if err := s.k8s.Restart(ctx); err != nil {
						slog.Error("applyAfterSync: restart failed", "configmap", cmName, "err", err)
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
		if cmName == "minecraft-fabric-mods" || cmName == "minecraft-fabric-configs" {
			depName = s.cfg.FabricDeployment
		} else {
			depName = s.cfg.MinecraftDeployment
		}
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
					if err := s.mck8s.RestartDeployment(ctx, depName); err != nil {
						slog.Error("applyMinecraftAfterSync: restart failed", "configmap", cmName, "dep", depName, "err", err)
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
