package admins

import (
	"context"
	"maps"
	"testing"

	"agrelha/internal/ports"
)

type memoryStateStore struct {
	doc ports.Document
}

func (m *memoryStateStore) Get(ctx context.Context, path string) (ports.Document, error) {
	return m.doc, nil
}

func (m *memoryStateStore) Put(ctx context.Context, path string, doc ports.Document, msg string) error {
	m.doc = doc
	return nil
}

func (m *memoryStateStore) Patch(ctx context.Context, path string, msg string, mutate func(doc *ports.Document) (bool, error)) (bool, error) {
	docCopy := ports.Document{
		Data:        make(map[string]string),
		Annotations: make(map[string]string),
	}
	maps.Copy(docCopy.Data, m.doc.Data)
	maps.Copy(docCopy.Annotations, m.doc.Annotations)
	changed, err := mutate(&docCopy)
	if err != nil || !changed {
		return false, err
	}
	m.doc = docCopy
	return true, nil
}

func (m *memoryStateStore) Delete(ctx context.Context, path string, msg string) error {
	m.doc = ports.Document{}
	return nil
}

func (m *memoryStateStore) PutTree(ctx context.Context, dirPath string, docs map[string]ports.Document, msg string) error {
	return nil
}

func TestAdminGrantAndRevoke(t *testing.T) {
	ctx := context.Background()
	store := &memoryStateStore{
		doc: ports.Document{
			Data: map[string]string{
				"ADMINLIST_IDS": "76561198000000001",
			},
		},
	}
	mgr := New(store, "manifests/valheim-admins.yaml")

	// 1. Grant existing -> no change
	changed, err := mgr.Grant(ctx, "76561198000000001")
	if err != nil {
		t.Fatalf("Grant error: %v", err)
	}
	if changed {
		t.Fatalf("expected changed=false when granting already-present admin")
	}

	// 2. Grant new admin
	changed, err = mgr.Grant(ctx, "76561198000000002")
	if err != nil {
		t.Fatalf("Grant error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true when granting new admin")
	}
	if store.doc.Data["ADMINLIST_IDS"] != "76561198000000001 76561198000000002" {
		t.Fatalf("unexpected ADMINLIST_IDS: %q", store.doc.Data["ADMINLIST_IDS"])
	}

	// 3. Revoke admin
	changed, err = mgr.Revoke(ctx, "76561198000000001")
	if err != nil {
		t.Fatalf("Revoke error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true when revoking admin")
	}
	if store.doc.Data["ADMINLIST_IDS"] != "76561198000000002" {
		t.Fatalf("unexpected ADMINLIST_IDS after revoke: %q", store.doc.Data["ADMINLIST_IDS"])
	}

	// 4. Revoke nonexistent -> no change
	changed, err = mgr.Revoke(ctx, "76561198000000099")
	if err != nil {
		t.Fatalf("Revoke error: %v", err)
	}
	if changed {
		t.Fatalf("expected changed=false when revoking nonexistent admin")
	}

	// 5. List from store
	list, err := mgr.List(ctx)
	if err != nil || len(list) != 1 || list[0] != "76561198000000002" {
		t.Errorf("expected [76561198000000002], got %v, err: %v", list, err)
	}
}

type mockAdminAuditRecorder struct {
	audits []string
}

func (a *mockAdminAuditRecorder) RecordAudit(actor, action, detail string) error {
	a.audits = append(a.audits, actor+":"+action+":"+detail)
	return nil
}

func TestAdminManagerOptions(t *testing.T) {
	ctx := context.Background()

	// 1. WithAdminReader
	mgrReader := New(nil, "", WithAdminReader(func(ctx context.Context) ([]string, error) {
		return []string{"123", "456"}, nil
	}))
	list, err := mgrReader.List(ctx)
	if err != nil || len(list) != 2 || list[0] != "123" {
		t.Errorf("unexpected list with reader: %v, err: %v", list, err)
	}

	// 2. WithAudit
	store := &memoryStateStore{doc: ports.Document{Data: map[string]string{}}}
	audit := &mockAdminAuditRecorder{}
	mgrAudit := New(store, "manifests/valheim-admins.yaml", WithAudit(audit))

	_, _ = mgrAudit.Grant(ctx, "999", "admin-user")
	if len(audit.audits) != 1 || audit.audits[0] != "admin-user:admin-grant:999" {
		t.Errorf("expected audit entry for grant: %v", audit.audits)
	}

	_, _ = mgrAudit.Revoke(ctx, "999", "admin-user")
	if len(audit.audits) != 2 || audit.audits[1] != "admin-user:admin-revoke:999" {
		t.Errorf("expected audit entry for revoke: %v", audit.audits)
	}

	// Actor fallback to "-"
	_, _ = mgrAudit.Grant(ctx, "777")
	if len(audit.audits) < 3 || audit.audits[2] != "-:admin-grant:777" {
		t.Errorf("expected '-' actor: %v", audit.audits)
	}
}

type failingStateStore struct {
	getErr   error
	patchErr error
}

func (f *failingStateStore) Get(ctx context.Context, path string) (ports.Document, error) {
	return ports.Document{}, f.getErr
}
func (f *failingStateStore) Put(ctx context.Context, path string, doc ports.Document, msg string) error {
	return nil
}
func (f *failingStateStore) Patch(ctx context.Context, path string, msg string, mutate func(doc *ports.Document) (bool, error)) (bool, error) {
	if f.patchErr != nil {
		return false, f.patchErr
	}
	// Call mutate on empty doc with nil Data to test nil doc.Data handling
	var doc ports.Document
	_, _ = mutate(&doc)
	return true, nil
}
func (f *failingStateStore) Delete(ctx context.Context, path string, msg string) error { return nil }
func (f *failingStateStore) PutTree(ctx context.Context, dirPath string, docs map[string]ports.Document, msg string) error {
	return nil
}

func TestAdminManagerErrorBranches(t *testing.T) {
	ctx := context.Background()

	// 1. Nil store returns ErrNotImplemented
	nilMgr := New(nil, "")
	if _, err := nilMgr.List(ctx); err != ports.ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented for List, got %v", err)
	}
	if _, err := nilMgr.Grant(ctx, "123"); err != ports.ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented for Grant, got %v", err)
	}
	if _, err := nilMgr.Revoke(ctx, "123"); err != ports.ErrNotImplemented {
		t.Errorf("expected ErrNotImplemented for Revoke, got %v", err)
	}

	// 2. Get error
	getFailMgr := New(&failingStateStore{getErr: context.Canceled}, "path")
	if _, err := getFailMgr.List(ctx); err != context.Canceled {
		t.Errorf("expected Get error, got %v", err)
	}

	// 3. Patch error
	patchFailMgr := New(&failingStateStore{patchErr: context.Canceled}, "path")
	if _, err := patchFailMgr.Grant(ctx, "123"); err != context.Canceled {
		t.Errorf("expected Grant patch error, got %v", err)
	}
	if _, err := patchFailMgr.Revoke(ctx, "123"); err != context.Canceled {
		t.Errorf("expected Revoke patch error, got %v", err)
	}

	// 4. Nil doc.Data
	nilDataMgr := New(&memoryStateStore{doc: ports.Document{Data: nil}}, "path")
	list, err := nilDataMgr.List(ctx)
	if err != nil || list != nil {
		t.Errorf("expected nil list for nil Data, got %v, err: %v", list, err)
	}

	// 5. Grant and Revoke on nil doc.Data
	nilDocStore := &failingStateStore{} // patchErr is nil, calls mutate with nil doc.Data
	nilDocMgr := New(nilDocStore, "path")
	_, _ = nilDocMgr.Grant(ctx, "123")
	_, _ = nilDocMgr.Revoke(ctx, "123")
}

