package requests

import (
	"context"
	"errors"
	"testing"

	"agrelha/internal/domain"
)

type fakeStore struct {
	rows map[string]domain.InstanceRequest
}

func newFakeStore() *fakeStore { return &fakeStore{rows: map[string]domain.InstanceRequest{}} }

func (f *fakeStore) PutRequest(_ context.Context, r domain.InstanceRequest) error {
	f.rows[r.ID] = r
	return nil
}

func (f *fakeStore) GetRequest(_ context.Context, id string) (*domain.InstanceRequest, error) {
	r, ok := f.rows[id]
	if !ok {
		return nil, nil
	}
	out := r
	return &out, nil
}

func (f *fakeStore) ListRequests(_ context.Context, status domain.RequestStatus) ([]domain.InstanceRequest, error) {
	var out []domain.InstanceRequest
	for _, r := range f.rows {
		if status == "" || r.Status == status {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeStore) ListRequestsFor(_ context.Context, subject string) ([]domain.InstanceRequest, error) {
	var out []domain.InstanceRequest
	for _, r := range f.rows {
		if r.Subject == subject {
			out = append(out, r)
		}
	}
	return out, nil
}

func listing(insts ...domain.Instance) ListFunc {
	return func(context.Context) ([]domain.Instance, error) { return insts, nil }
}

func TestMayCreateDirectlyCountsOnlyWorldsTheyCreated(t *testing.T) {
	// friend created nothing; world 1 is mine, world 2 was merely granted to them.
	svc := New(newFakeStore(), WithList(listing(
		domain.Instance{GameID: domain.GameValheim, Number: 1, CreatedBy: "me"},
		domain.Instance{GameID: domain.GameValheim, Number: 2, CreatedBy: "me"},
	)))

	ok, err := svc.MayCreateDirectly(context.Background(), "friend")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("an account that has created nothing may create its first world directly")
	}
}

func TestMayCreateDirectlyFalseOnceTheyOwnOne(t *testing.T) {
	svc := New(newFakeStore(), WithList(listing(
		domain.Instance{GameID: domain.GameValheim, Number: 3, CreatedBy: "friend"},
	)))

	ok, err := svc.MayCreateDirectly(context.Background(), "friend")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("a second world must go through approval")
	}
}

func TestSubmitRefusesASecondPendingRequest(t *testing.T) {
	svc := New(newFakeStore(), WithList(listing()))
	inst := domain.Instance{GameID: domain.GameValheim, Name: "One"}

	if _, err := svc.Submit(context.Background(), "friend", inst, domain.ModList{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Submit(context.Background(), "friend", inst, domain.ModList{}); err == nil {
		t.Error("a second pending request must be refused")
	}
}

func TestApproveCreatesAndGrants(t *testing.T) {
	store := newFakeStore()
	var createdBy, grantedTo string
	var grantedNum int

	svc := New(store,
		WithList(listing()),
		WithCreate(func(_ context.Context, game domain.GameID, inst domain.Instance, _ domain.ModList, _ string) (*domain.Instance, error) {
			createdBy = inst.CreatedBy
			out := inst
			out.Number = 7
			out.GameID = game
			return &out, nil
		}),
		WithGrant(func(_ context.Context, subject string, _ domain.GameID, number int, _ string) error {
			grantedTo, grantedNum = subject, number
			return nil
		}),
	)

	req, err := svc.Submit(context.Background(), "friend", domain.Instance{GameID: domain.GameValheim, Name: "Two"}, domain.ModList{})
	if err != nil {
		t.Fatal(err)
	}

	created, err := svc.Approve(context.Background(), req.ID, "me")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if created.Number != 7 {
		t.Errorf("created number = %d", created.Number)
	}
	if createdBy != "friend" {
		t.Errorf("the world must record the requester as creator, got %q", createdBy)
	}
	if grantedTo != "friend" || grantedNum != 7 {
		t.Errorf("requester must be granted the new world, got %q/%d", grantedTo, grantedNum)
	}

	after, _ := store.GetRequest(context.Background(), req.ID)
	if after.Status != domain.RequestApproved || after.Number != 7 {
		t.Errorf("request not marked approved: %+v", after.Status)
	}
}

func TestApproveTwiceIsRefused(t *testing.T) {
	store := newFakeStore()
	svc := New(store,
		WithList(listing()),
		WithCreate(func(_ context.Context, game domain.GameID, inst domain.Instance, _ domain.ModList, _ string) (*domain.Instance, error) {
			out := inst
			out.Number = 1
			out.GameID = game
			return &out, nil
		}),
	)
	req, err := svc.Submit(context.Background(), "friend", domain.Instance{GameID: domain.GameValheim, Name: "X"}, domain.ModList{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(context.Background(), req.ID, "me"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Approve(context.Background(), req.ID, "me")
	if !errors.Is(err, domain.ErrRequestNotPending) {
		t.Errorf("want ErrRequestNotPending, got %v", err)
	}
}

func TestApproveDoesNotCreateWhenCreationFails(t *testing.T) {
	store := newFakeStore()
	svc := New(store,
		WithList(listing()),
		WithCreate(func(context.Context, domain.GameID, domain.Instance, domain.ModList, string) (*domain.Instance, error) {
			return nil, errors.New("budget exhausted")
		}),
	)
	req, _ := svc.Submit(context.Background(), "friend", domain.Instance{GameID: domain.GameValheim, Name: "Y"}, domain.ModList{})

	if _, err := svc.Approve(context.Background(), req.ID, "me"); err == nil {
		t.Fatal("expected the approval to fail")
	}
	after, _ := store.GetRequest(context.Background(), req.ID)
	if !after.Pending() {
		t.Error("a failed approval must leave the request pending, not consume it")
	}
}

func TestDenyRecordsTheReason(t *testing.T) {
	store := newFakeStore()
	svc := New(store, WithList(listing()))
	req, _ := svc.Submit(context.Background(), "friend", domain.Instance{GameID: domain.GameValheim, Name: "Z"}, domain.ModList{})

	if err := svc.Deny(context.Background(), req.ID, "no budget right now", "me"); err != nil {
		t.Fatal(err)
	}
	after, _ := store.GetRequest(context.Background(), req.ID)
	if after.Status != domain.RequestDenied || after.Note != "no budget right now" {
		t.Errorf("deny not recorded: %+v", after)
	}
	if err := svc.Deny(context.Background(), req.ID, "", "me"); !errors.Is(err, domain.ErrRequestNotPending) {
		t.Errorf("denying twice must be refused, got %v", err)
	}
}

func TestCreationLimitIsConfigurable(t *testing.T) {
	owned := listing(domain.Instance{GameID: domain.GameValheim, Number: 1, CreatedBy: "friend"})

	// Default allowance of one: a second world needs approval.
	one := New(newFakeStore(), WithList(owned))
	if ok, err := one.MayCreateDirectly(context.Background(), "friend"); err != nil || ok {
		t.Errorf("with an allowance of 1, a second world must need approval (ok=%v, err=%v)", ok, err)
	}

	// Raised to two: the same account may create directly.
	two := New(newFakeStore(), WithList(owned),
		WithCreationLimit(func(context.Context) int { return 2 }))
	if ok, err := two.MayCreateDirectly(context.Background(), "friend"); err != nil || !ok {
		t.Errorf("with an allowance of 2, a second world must be allowed (ok=%v, err=%v)", ok, err)
	}

	// Zero: even a first world needs approval.
	zero := New(newFakeStore(), WithList(listing()),
		WithCreationLimit(func(context.Context) int { return 0 }))
	if ok, err := zero.MayCreateDirectly(context.Background(), "newcomer"); err != nil || ok {
		t.Errorf("with an allowance of 0, every world must need approval (ok=%v, err=%v)", ok, err)
	}
}
