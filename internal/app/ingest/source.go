package ingest

import (
	"context"
	"fmt"
	"io"
)

type InstanceSource struct {
	logs   func(ctx context.Context, instanceNumber int, tail int64) (io.ReadCloser, error)
	target func(ctx context.Context) (int, error)
}

func NewInstanceSource(
	logs func(ctx context.Context, instanceNumber int, tail int64) (io.ReadCloser, error),
	target func(ctx context.Context) (int, error),
) InstanceSource {
	return InstanceSource{logs: logs, target: target}
}

func (s InstanceSource) StreamLogs(ctx context.Context, tail int64) (io.ReadCloser, error) {
	if s.logs == nil || s.target == nil {
		return nil, fmt.Errorf("ingest: no log source configured")
	}
	instanceNumber, err := s.target(ctx)
	if err != nil {
		return nil, err
	}
	return s.logs(ctx, instanceNumber, tail)
}
