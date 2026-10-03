package ports

import (
	"context"

	"agrelha/internal/domain"
)

type InstanceRequests interface {
	PutRequest(ctx context.Context, r domain.InstanceRequest) error
	GetRequest(ctx context.Context, id string) (*domain.InstanceRequest, error)
	ListRequests(ctx context.Context, status domain.RequestStatus) ([]domain.InstanceRequest, error)
	ListRequestsFor(ctx context.Context, subject string) ([]domain.InstanceRequest, error)
}
