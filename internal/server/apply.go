package server

import (
	"context"
	"log"
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
				log.Printf("applyAfterSync(%s): timed out waiting for ArgoCD sync", cmName)
				return
			case <-t.C:
				data, err := s.k8s.ConfigMapData(ctx, cmName)
				if err != nil {
					continue
				}
				if want(data[key]) {
					if err := s.k8s.Restart(ctx); err != nil {
						log.Printf("applyAfterSync(%s): restart failed: %v", cmName, err)
						return
					}
					_ = s.store.RecordEvent("auto-restart", cmName)
					return
				}
			}
		}
	}()
}
