package content

import (
	"context"
	"testing"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

type fakeCatalog struct {
	items map[string]domain.ModSearchResult
}

func (f *fakeCatalog) Get(fullName string) (domain.ModSearchResult, bool) {
	m, ok := f.items[fullName]
	return m, ok
}
func (f *fakeCatalog) Search(ctx context.Context, query string, limit int) ([]domain.ModSearchResult, error) {
	return nil, nil
}
func (f *fakeCatalog) Ready() bool { return true }
func (f *fakeCatalog) LatestVersion(ctx context.Context, ns, name string) (string, []string, error) {
	return "", nil, nil
}
func (f *fakeCatalog) Readme(ctx context.Context, ns, name, version string) (string, error) {
	return "", nil
}
func (f *fakeCatalog) ResolveTree(ctx context.Context, ns, name string) ([]string, error) {
	return nil, nil
}

type fakeAudit struct{}

func (f *fakeAudit) RecordAudit(actor, action, detail string) error { return nil }

func TestPublicHost(t *testing.T) {
	if PublicHost("127.0.0.1") {
		t.Errorf("expected 127.0.0.1 to not be public")
	}
	if PublicHost("localhost") {
		t.Errorf("expected localhost to not be public")
	}
	if PublicHost("") {
		t.Errorf("expected empty host to not be public")
	}
}

func TestConfigValidation(t *testing.T) {
	valid := []string{"foo.cfg", "com.author.mod.cfg", "My_Mod-1.cfg"}
	for _, f := range valid {
		if !cfgNameRe.MatchString(f) {
			t.Errorf("expected %q to be valid config file name", f)
		}
	}
	invalid := []string{"foo.txt", "../foo.cfg", "foo/bar.cfg", ".hidden.cfg"}
	for _, f := range invalid {
		if cfgNameRe.MatchString(f) {
			t.Errorf("expected %q to be invalid config file name", f)
		}
	}
}

type fakeValheimGame struct {
	bundle domain.Bundle
}

func (f *fakeValheimGame) ID() domain.GameID                  { return domain.GameValheim }
func (f *fakeValheimGame) Display() domain.Display            { return domain.Display{Name: "Valheim"} }
func (f *fakeValheimGame) Providers() []ports.ContentProvider { return nil }
func (f *fakeValheimGame) ResolveContent(ctx context.Context, inst domain.Instance) (domain.ContentSet, error) {
	return domain.ContentSet{}, nil
}
func (f *fakeValheimGame) ExportClientBundle(ctx context.Context, inst domain.Instance) (domain.Bundle, error) {
	return f.bundle, nil
}
func (f *fakeValheimGame) RuntimeSpec(inst domain.Instance) domain.RuntimeSpec {
	return domain.RuntimeSpec{}
}
func (f *fakeValheimGame) Telemetry(ctx context.Context) (domain.GameTelemetry, error) {
	return domain.GameTelemetry{}, nil
}
func (f *fakeValheimGame) AdmissionModel() domain.AdmissionModel { return domain.AdmissionPassword }
func (f *fakeValheimGame) OperatorIDKind() domain.OperatorIDKind { return domain.IDKindSteam64 }
