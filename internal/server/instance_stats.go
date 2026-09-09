package server

import (
	"context"

	"agrelha/internal/minecraft"
)

type instanceStat struct {
	Players      int
	PlayersKnown bool
	Uptime       string
}

func (s *FiberServer) instanceStats(ctx context.Context, insts []minecraft.Instance) map[int]instanceStat {
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
