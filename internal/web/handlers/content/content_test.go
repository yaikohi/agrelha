package content

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/mods"
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

func TestModpackExport(t *testing.T) {
	h := New(Config{
		Audit: &fakeAudit{},
		InstalledMods: func(context.Context) ([]string, error) {
			return mods.Parse("# my server\ndenikson/BepInExPack_Valheim/5.4.2202\nValheimModding/Jotunn/2.24.3\n"), nil
		},
		ConfigData: func(context.Context) (map[string]string, error) {
			return map[string]string{"com.jotunn.jotunn.cfg": "[General]\nEnabled = true\n"}, nil
		},
	})

	app := fiber.New()
	h.Register(app)

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/mods/export", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("content-type = %q", ct)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.Contains(cd, ".r2z") {
		t.Fatalf("content-disposition = %q", cd)
	}

	blob, _ := io.ReadAll(resp.Body)
	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = string(b)
	}
	r2x, ok := files["export.r2x"]
	if !ok {
		t.Fatalf("no export.r2x; files=%v", files)
	}
	for _, want := range []string{
		"profileName: Valheim (ykhi)",
		"name: denikson-BepInExPack_Valheim",
		"name: ValheimModding-Jotunn",
		"major: 5", "minor: 4", "patch: 2202",
		"enabled: true",
	} {
		if !strings.Contains(r2x, want) {
			t.Fatalf("export.r2x missing %q:\n%s", want, r2x)
		}
	}
	if _, ok := files["config/com.jotunn.jotunn.cfg"]; !ok {
		t.Fatalf("config not under config/; files=%v", files)
	}
}

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

func TestModpackExportWithValheimGame(t *testing.T) {
	g := &fakeValheimGame{
		bundle: domain.Bundle{
			Filename:    "valheim-custom.r2z",
			ContentType: "application/zip",
			Data:        []byte("mock-r2z-archive"),
		},
	}

	h := New(Config{
		ValheimGame: g,
	})

	app := fiber.New()
	app.Get("/mods/export", h.ModpackExport)

	req := httptest.NewRequest("GET", "/mods/export", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("GET /mods/export: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "mock-r2z-archive" {
		t.Errorf("body = %s, want mock-r2z-archive", string(body))
	}
	if disp := resp.Header.Get("Content-Disposition"); !strings.Contains(disp, "valheim-custom.r2z") {
		t.Errorf("Content-Disposition = %s, want valheim-custom.r2z", disp)
	}
}
