package ingest

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestTheIngestSourceStreamsFromTheResolvedInstance(t *testing.T) {
	var askedFor int
	src := NewInstanceSource(
		func(ctx context.Context, num int, tail int64) (io.ReadCloser, error) {
			askedFor = num
			return io.NopCloser(strings.NewReader("line\n")), nil
		},
		func(ctx context.Context) (int, error) { return 2, nil },
	)

	rc, err := src.StreamLogs(context.Background(), 200)
	if err != nil {
		t.Fatalf("StreamLogs: %v", err)
	}
	defer rc.Close()
	if askedFor != 2 {
		t.Errorf("the source streamed instance %d, not the resolved target 2;\n"+
			"presence ingestion must follow whichever Instance the resolver names, because its Host decides\n"+
			"whether the lines come from a pod or a container", askedFor)
	}
}

func TestTheIngestSourceReportsAnUnresolvableTarget(t *testing.T) {
	want := errors.New("no valheim instance")
	src := NewInstanceSource(
		func(ctx context.Context, num int, tail int64) (io.ReadCloser, error) {
			t.Error("streamed logs despite having no target instance")
			return nil, nil
		},
		func(ctx context.Context) (int, error) { return 0, want },
	)

	if _, err := src.StreamLogs(context.Background(), 200); !errors.Is(err, want) {
		t.Errorf("an unresolvable target must surface as an error so Run backs off; got %v", err)
	}
}

func TestTheIngestSourceRefusesWhenUnconfigured(t *testing.T) {
	if _, err := (InstanceSource{}).StreamLogs(context.Background(), 200); err == nil {
		t.Error("a zero InstanceSource returned no error; an unconfigured source must not look like an empty log stream")
	}
}

func TestRetryBackoffIsCappedAndClimbs(t *testing.T) {
	if MaxRetryDelay <= RetryDelay {
		t.Fatalf("MaxRetryDelay %v must exceed RetryDelay %v", MaxRetryDelay, RetryDelay)
	}
	delay := RetryDelay
	for range 20 {
		if delay *= 2; delay > MaxRetryDelay {
			delay = MaxRetryDelay
		}
	}
	if delay != MaxRetryDelay {
		t.Errorf("backoff settled at %v, not the %v cap", delay, MaxRetryDelay)
	}
	if RetryDelay*2 > MaxRetryDelay {
		t.Errorf("the first backoff step already hits the cap, so a failing stream never slows down")
	}
}
