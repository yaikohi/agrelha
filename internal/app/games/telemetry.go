package games

import (
	"context"
	"fmt"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

func CollectTelemetry(ctx context.Context, rt ports.Runtime, ref ports.ServerRef) domain.GameTelemetry {
	tele := domain.GameTelemetry{
		Lifecycle:    "unknown",
		Online:       false,
		CPU:          "—",
		Memory:       "—",
		Uptime:       "—",
		PlayersKnown: false,
	}

	if rt == nil {
		return tele
	}

	if st, err := rt.Status(ctx, ref); err == nil {
		if st.Lifecycle != "" {
			tele.Lifecycle = string(st.Lifecycle)
		}
		tele.Online = st.Available
		if !st.StartedAt.IsZero() {
			tele.StartedAt = st.StartedAt
			tele.Uptime = domain.FormatDuration(time.Since(st.StartedAt))
		}
	}
	if m, err := rt.Metrics(ctx, ref); err == nil && m.Known {
		tele.CPU = fmt.Sprintf("%dm", m.CPUMillicores)
		tele.Memory = fmt.Sprintf("%d Mi", m.MemoryMiB)
	}

	return tele
}
