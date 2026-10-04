package requests

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

// FreeCreations is the fallback when no limit has been configured. The live
// value is a global setting owned by app/capacity, injected via
// WithCreationLimit, and counts worlds across every game.
const FreeCreations = 1

type CreateFunc func(ctx context.Context, game domain.GameID, inst domain.Instance, mods domain.ModList, actor string) (*domain.Instance, error)

type GrantFunc func(ctx context.Context, subject string, game domain.GameID, number int, actor string) error

type ListFunc func(ctx context.Context) ([]domain.Instance, error)

type Service struct {
	store  ports.InstanceRequests
	create CreateFunc
	grant  GrantFunc
	list   ListFunc
	audit  ports.AuditRecorder
	limit  func(context.Context) int
	now    func() time.Time
}

type Option func(*Service)

func WithCreate(fn CreateFunc) Option { return func(s *Service) { s.create = fn } }
func WithGrant(fn GrantFunc) Option   { return func(s *Service) { s.grant = fn } }
func WithList(fn ListFunc) Option     { return func(s *Service) { s.list = fn } }

func WithAudit(a ports.AuditRecorder) Option { return func(s *Service) { s.audit = a } }

func WithCreationLimit(fn func(context.Context) int) Option {
	return func(s *Service) { s.limit = fn }
}

func WithClock(fn func() time.Time) Option { return func(s *Service) { s.now = fn } }

func New(store ports.InstanceRequests, opts ...Option) *Service {
	s := &Service{store: store, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Service) CreatedBy(ctx context.Context, subject string) (int, error) {
	if s == nil || s.list == nil || subject == "" {
		return 0, nil
	}
	all, err := s.list(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, inst := range all {
		if inst.CreatedBy == subject {
			n++
		}
	}
	return n, nil
}

func (s *Service) CreationLimit(ctx context.Context) int {
	if s == nil || s.limit == nil {
		return FreeCreations
	}
	n := s.limit(ctx)
	if n < 0 {
		return 0
	}
	return n
}

func (s *Service) MayCreateDirectly(ctx context.Context, subject string) (bool, error) {
	n, err := s.CreatedBy(ctx, subject)
	if err != nil {
		return false, err
	}
	return n < s.CreationLimit(ctx), nil
}

func (s *Service) Submit(ctx context.Context, subject string, inst domain.Instance, mods domain.ModList) (*domain.InstanceRequest, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("submit: no request store configured")
	}
	if subject == "" {
		return nil, errors.New("submit: empty subject")
	}
	open, err := s.store.ListRequestsFor(ctx, subject)
	if err != nil {
		return nil, err
	}
	for _, r := range open {
		if r.Pending() {
			return nil, fmt.Errorf("you already have a pending request for %q", r.Name)
		}
	}
	req := domain.InstanceRequest{
		ID:        newID(),
		Subject:   subject,
		GameID:    inst.GameID,
		Name:      inst.Name,
		Instance:  inst,
		Mods:      mods,
		Status:    domain.RequestPending,
		CreatedAt: s.now(),
	}
	if err := s.store.PutRequest(ctx, req); err != nil {
		return nil, err
	}
	s.record(subject, "instance-request-submit", req.Label())
	return &req, nil
}

func (s *Service) Pending(ctx context.Context) ([]domain.InstanceRequest, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	return s.store.ListRequests(ctx, domain.RequestPending)
}

func (s *Service) All(ctx context.Context) ([]domain.InstanceRequest, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	return s.store.ListRequests(ctx, "")
}

func (s *Service) ForSubject(ctx context.Context, subject string) ([]domain.InstanceRequest, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	return s.store.ListRequestsFor(ctx, subject)
}

func (s *Service) Approve(ctx context.Context, id, actor string) (*domain.Instance, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("approve: no request store configured")
	}
	if s.create == nil {
		return nil, errors.New("approve: no instance creator configured")
	}
	req, err := s.store.GetRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.New("no such request")
	}
	if !req.Pending() {
		return nil, domain.ErrRequestNotPending
	}

	inst := req.Instance
	inst.CreatedBy = req.Subject
	created, err := s.create(ctx, req.GameID, inst, req.Mods, actor)
	if err != nil {
		return nil, err
	}

	if s.grant != nil {
		if err := s.grant(ctx, req.Subject, created.GameID, created.Number, actor); err != nil {
			return created, fmt.Errorf("world created but access could not be granted: %w", err)
		}
	}

	req.Status = domain.RequestApproved
	req.DecidedAt = s.now()
	req.DecidedBy = actor
	req.Number = created.Number
	if err := s.store.PutRequest(ctx, *req); err != nil {
		return created, err
	}
	s.record(actor, "instance-request-approve", req.Label())
	return created, nil
}

func (s *Service) Deny(ctx context.Context, id, note, actor string) error {
	if s == nil || s.store == nil {
		return errors.New("deny: no request store configured")
	}
	req, err := s.store.GetRequest(ctx, id)
	if err != nil {
		return err
	}
	if req == nil {
		return errors.New("no such request")
	}
	if !req.Pending() {
		return domain.ErrRequestNotPending
	}
	req.Status = domain.RequestDenied
	req.DecidedAt = s.now()
	req.DecidedBy = actor
	req.Note = note
	if err := s.store.PutRequest(ctx, *req); err != nil {
		return err
	}
	s.record(actor, "instance-request-deny", req.Label())
	return nil
}

func (s *Service) record(actor, action, detail string) {
	if s.audit == nil {
		return
	}
	if actor == "" {
		actor = "-"
	}
	_ = s.audit.RecordAudit(actor, action, detail)
}

func (s *Service) GrantCreator(ctx context.Context, subject string, game domain.GameID, number int, actor string) error {
	if s == nil || s.grant == nil || subject == "" {
		return nil
	}
	return s.grant(ctx, subject, game, number, actor)
}
