package rcon

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExecuteReportsTheDiscardedErrorToTheInjectedTracer(t *testing.T) {
	var gotName string
	var gotErr error
	called := false

	tracer := func(ctx context.Context, name string) (context.Context, func(error)) {
		gotName = name
		called = true
		return ctx, func(err error) { gotErr = err }
	}

	c := NewClient("127.0.0.1:1", "pw", 20*time.Millisecond, WithTracer(tracer))

	if _, err := c.Execute(context.Background(), "/op someone"); err == nil {
		t.Fatal("expected a connection error against a dead address")
	}
	if !called {
		t.Fatal("tracer was not invoked")
	}
	if gotName != "rcon.execute /op" {
		t.Errorf("span name = %q, want %q", gotName, "rcon.execute /op")
	}
	if gotErr == nil {
		t.Error("tracer finish got nil error; a failing command must be reported even though callers discard it")
	}
}

func TestExecuteSucceedsWithNoTracerInjected(t *testing.T) {
	c := NewClient("127.0.0.1:1", "pw", 20*time.Millisecond)
	if _, err := c.Execute(context.Background(), "/list"); err == nil {
		t.Fatal("expected a connection error")
	}
}

func TestExecuteHonoursACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := NewClient("127.0.0.1:1", "pw", time.Second)
	_, err := c.Execute(ctx, "/list")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
