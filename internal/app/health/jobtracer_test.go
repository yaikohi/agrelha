package health

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestStartRunsEachTickThroughTheInjectedJobTracer(t *testing.T) {
	var mu sync.Mutex
	var names []string
	ran := make(chan struct{}, 1)

	tracer := func(ctx context.Context, name string, run func(context.Context)) {
		mu.Lock()
		names = append(names, name)
		mu.Unlock()
		run(ctx)
		select {
		case ran <- struct{}{}:
		default:
		}
	}

	w := New(&fakeRecorder{}, []Source{&fakeSource{}},
		WithInterval(5*time.Millisecond),
		WithJobTracer(tracer))

	ctx := t.Context()
	w.Start(ctx)

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("job tracer was never invoked; Start does not route ticks through it")
	}

	mu.Lock()
	defer mu.Unlock()
	if names[0] != "health.check" {
		t.Errorf("span name = %q, want %q", names[0], "health.check")
	}
}

func TestWatcherWithNoJobTracerStillTicks(t *testing.T) {
	w := New(&fakeRecorder{}, []Source{&fakeSource{}}, WithInterval(5*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	w.Start(ctx)
	<-ctx.Done()
}
