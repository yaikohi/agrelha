package ingest

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"agrelha/internal/domain"
)

type InstanceLister interface {
	ListInstances(ctx context.Context) ([]domain.Instance, error)
}

type InstanceLogStreamer interface {
	InstanceLogs(ctx context.Context, num int, tail int64) (io.ReadCloser, error)
}

type Supervisor struct {
	lister    InstanceLister
	streamer  InstanceLogStreamer
	store     presenceStore
	pollDelay time.Duration
	mu        sync.Mutex
	workers   map[int]context.CancelFunc
	wg        sync.WaitGroup
}

func NewSupervisor(lister InstanceLister, streamer InstanceLogStreamer, store presenceStore, pollDelay time.Duration) *Supervisor {
	return &Supervisor{
		lister:    lister,
		streamer:  streamer,
		store:     store,
		pollDelay: pollDelay,
		workers:   make(map[int]context.CancelFunc),
	}
}

func (s *Supervisor) Run(ctx context.Context) {
	ticker := time.NewTicker(s.pollDelay)
	defer ticker.Stop()
	defer s.stopAll()

	s.Reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Reconcile(ctx)
		}
	}
}

func (s *Supervisor) Reconcile(ctx context.Context) {
	if s.lister == nil || s.streamer == nil || s.store == nil {
		return
	}
	insts, err := s.lister.ListInstances(ctx)
	if err != nil {
		slog.Warn("ingest supervisor: failed to list instances", "err", err)
		return
	}

	active := make(map[int]bool)
	for _, inst := range insts {
		if inst.State == domain.StateRunning {
			active[inst.Number] = true
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for num := range active {
		if _, exists := s.workers[num]; !exists {
			wCtx, cancel := context.WithCancel(ctx)
			s.workers[num] = cancel
			streamer := logStreamerFunc(func(c context.Context, tail int64) (io.ReadCloser, error) {
				return s.streamer.InstanceLogs(c, num, tail)
			})
			s.wg.Add(1)
			go func(n int, workerCtx context.Context) {
				defer s.wg.Done()
				Run(workerCtx, n, streamer, s.store)
			}(num, wCtx)
		}
	}

	for num, cancel := range s.workers {
		if !active[num] {
			cancel()
			delete(s.workers, num)
		}
	}
}

func (s *Supervisor) ActiveWorkers() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := make([]int, 0, len(s.workers))
	for num := range s.workers {
		res = append(res, num)
	}
	return res
}

func (s *Supervisor) stopAll() {
	s.mu.Lock()
	for num, cancel := range s.workers {
		cancel()
		delete(s.workers, num)
	}
	s.mu.Unlock()
	s.wg.Wait()
}

type logStreamerFunc func(ctx context.Context, tail int64) (io.ReadCloser, error)

func (f logStreamerFunc) StreamLogs(ctx context.Context, tail int64) (io.ReadCloser, error) {
	return f(ctx, tail)
}
