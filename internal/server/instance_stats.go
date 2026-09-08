package server

import (
	"context"
	"sync"
	"time"

	"agrelha/internal/minecraft"
)

// instanceStatsTTL bounds how often the dashboard reaches out to RCON and the
// k8s API. The dashboard is served UNAUTHENTICATED to LAN/WireGuard players, so
// stats must be cached rather than gathered per request — otherwise every page
// load opens an RCON connection to every running world.
const instanceStatsTTL = 15 * time.Second

type instanceStat struct {
	Players      int
	PlayersKnown bool
	Uptime       string
}

type instanceStatsCache struct {
	mu   sync.Mutex
	at   time.Time
	data map[int]instanceStat
}

// instanceStats returns per-instance player counts and uptimes for the running
// instances, cached for instanceStatsTTL. A world that cannot be reached simply
// reports PlayersKnown=false rather than failing the page.
func (s *FiberServer) instanceStats(ctx context.Context, insts []minecraft.Instance) map[int]instanceStat {
	s.instStats.mu.Lock()
	defer s.instStats.mu.Unlock()

	if s.instStats.data != nil && time.Since(s.instStats.at) < instanceStatsTTL {
		return s.instStats.data
	}

	out := make(map[int]instanceStat, len(insts))
	for _, inst := range insts {
		if inst.State != minecraft.StateRunning {
			continue
		}
		st := instanceStat{}

		if s.mck8s != nil {
			if ps, err := s.mck8s.DeploymentPodStatus(ctx, inst.DeploymentName()); err == nil && !ps.StartedAt.IsZero() {
				st.Uptime = humanDuration(time.Since(ps.StartedAt))
			}
		}
		if s.mcRconPool != nil && inst.LBIP != "" {
			if res, err := s.mcRconPool.ClientFor(inst.LBIP + ":25575").Execute("/list"); err == nil {
				st.Players = len(minecraft.ParsePlayerList(res))
				st.PlayersKnown = true
			}
		}
		out[inst.Number] = st
	}

	s.instStats.data = out
	s.instStats.at = time.Now()
	return out
}
