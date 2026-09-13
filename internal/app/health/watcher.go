// Package health turns what the runtime knows about a dead server into a
// recorded Incident. It answers "what happened?", which no readiness probe can:
// by the time anyone looks, the container has been replaced.
package health

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

const (
	defaultInterval = 30 * time.Second
	defaultTail     = 100
	maxTailBytes    = 64 << 10
)

// Source is one game's instances, as seen by the watcher.
type Source interface {
	GameID() domain.GameID
	ListInstances(ctx context.Context) ([]domain.Instance, error)
	RuntimeStatus(ctx context.Context, num int) (ports.Status, error)
	CrashLogs(ctx context.Context, num int, tail int64, step ...string) (io.ReadCloser, error)
}

// Recorder persists what the watcher finds.
type Recorder interface {
	LastIncident(ctx context.Context, game domain.GameID, number int) (*domain.Incident, error)
	RecordIncident(ctx context.Context, in domain.Incident) (int64, error)
	RecordEvent(kind, detail string) error
}

type Watcher struct {
	rec      Recorder
	sources  []Source
	interval time.Duration
	tail     int64
}

type Option func(*Watcher)

func WithInterval(d time.Duration) Option {
	return func(w *Watcher) {
		if d > 0 {
			w.interval = d
		}
	}
}

func New(rec Recorder, sources []Source, opts ...Option) *Watcher {
	w := &Watcher{rec: rec, sources: sources, interval: defaultInterval, tail: defaultTail}
	for _, o := range opts {
		o(w)
	}
	return w
}

func (w *Watcher) Start(ctx context.Context) {
	if w == nil || w.rec == nil || len(w.sources) == 0 {
		return
	}
	go func() {
		t := time.NewTicker(w.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				w.Check(ctx)
			}
		}
	}()
}

// Check scans every instance once and records any newly observed failure.
// It returns how many incidents it recorded, so a test can assert on it.
func (w *Watcher) Check(ctx context.Context) int {
	recorded := 0
	for _, src := range w.sources {
		instances, err := src.ListInstances(ctx)
		if err != nil {
			slog.Warn("health: cannot list instances", "game", src.GameID(), "err", err)
			continue
		}
		for _, inst := range instances {
			if w.checkOne(ctx, src, inst) {
				recorded++
			}
		}
	}
	return recorded
}

func (w *Watcher) checkOne(ctx context.Context, src Source, inst domain.Instance) bool {
	st, err := src.RuntimeStatus(ctx, inst.Number)
	if err != nil || !st.Failure.Crashed() {
		return false
	}

	last, err := w.rec.LastIncident(ctx, src.GameID(), inst.Number)
	if err != nil {
		slog.Warn("health: cannot read last incident", "game", src.GameID(), "instance", inst.Number, "err", err)
		return false
	}
	if !isNew(st.Failure, last) {
		return false
	}

	in := domain.Incident{
		GameID:       src.GameID(),
		Number:       inst.Number,
		At:           time.Now(),
		RestartCount: restartCount(st.Failure),
		ExitCode:     st.Failure.ExitCode,
		Reason:       failureReason(st.Failure),
		OOMKilled:    st.Failure.OOMKilled,
		Step:         st.Failure.InitStep,
		LogTail:      w.captureTail(ctx, src, inst.Number, st.Failure.InitStep),
	}

	if _, err := w.rec.RecordIncident(ctx, in); err != nil {
		slog.Error("health: cannot record incident", "game", src.GameID(), "instance", inst.Number, "err", err)
		return false
	}
	_ = w.rec.RecordEvent("crash", inst.Name+": "+in.Summary())
	slog.Warn("health: instance failure recorded", "game", src.GameID(), "instance", inst.Number, "summary", in.Summary())
	return true
}

func isNew(f ports.Failure, last *domain.Incident) bool {
	if last == nil {
		return true
	}
	if restartCount(f) > last.RestartCount {
		return true
	}
	if f.InitStep != last.Step {
		return true
	}
	return f.FinishedAt.After(last.At)
}

// restartCount reports the attempts that matter for this failure. A setup step
// that never let the server start is counted on its own tally.
func restartCount(f ports.Failure) int32 {
	if f.FailedBeforeStart() {
		return f.InitRestartCount
	}
	return f.RestartCount
}

func failureReason(f ports.Failure) string {
	if f.WaitingReason != "" {
		return f.WaitingReason
	}
	return f.Reason
}

func (w *Watcher) captureTail(ctx context.Context, src Source, num int, step string) string {
	rc, err := src.CrashLogs(ctx, num, w.tail, step)
	if err != nil {
		return ""
	}
	defer rc.Close()

	b, err := io.ReadAll(io.LimitReader(rc, maxTailBytes))
	if err != nil && len(b) == 0 {
		return ""
	}
	return strings.TrimRight(string(b), "\n")
}
