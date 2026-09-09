package server

import (
	"agrelha/internal/domain"
	"context"
)

type instanceStat struct {
	Players      int
	PlayersKnown bool
	Uptime       string
}

func (s *FiberServer) instanceStats(ctx context.Context, insts []domain.Instance) map[int]instanceStat {
	raw := s.ensureInstancesHandler().InstanceStats(ctx, insts)
	out := make(map[int]instanceStat, len(raw))
	for k, v := range raw {
		out[k] = instanceStat{
			Players:      v.Players,
			PlayersKnown: v.PlayersKnown,
			Uptime:       v.Uptime,
		}
	}
	return out
}
