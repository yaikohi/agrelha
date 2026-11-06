package tracing

import (
	"context"
	"testing"
)

func TestInitEnabled(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	ctx := context.Background()
	shutdown, err := Init(ctx, "0.1.0", "abc1234")
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	defer func() {
		_ = shutdown(ctx)
	}()
}
