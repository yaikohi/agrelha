package content

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"agrelha/internal/infra/content/thunderstore"
	"agrelha/internal/infra/kube"
	"agrelha/internal/infra/store"
	"agrelha/internal/platform/config"
)

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"2.25.0", "2.24.3", true},
		{"2.24.3", "2.24.3", false},
		{"2.24.3", "2.25.0", false},
		{"1.0.0", "0.9.9", true},
		{"5.4.2202", "5.4.900", true},
		{"1.2", "1.2.0", false},
		{"1.2.1", "1.2", true},
	}
	for _, c := range cases {
		if got := VersionNewer(c.a, c.b); got != c.want {
			t.Errorf("VersionNewer(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestPendingActive(t *testing.T) {
	mk := func(modsTxt string) *Handler {
		cs := fake.NewSimpleClientset(&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim-mods", Namespace: "valheim"},
			Data:       map[string]string{"mods.txt": modsTxt},
		})
		return New(Config{
			Cfg: &config.Config{},
			K8s: k8s.NewWithClientset(cs, "valheim", "valheim"),
		})
	}
	committed := []string{"ValheimModding/Jotunn/2.25.0", "denikson/BepInExPack_Valheim/5.4.2202"}

	// CM still holds old version -> pending
	h := mk("denikson/BepInExPack_Valheim/5.4.2202\nValheimModding/Jotunn/2.24.3\n")
	h.SetPending(committed)
	if !h.PendingActive(context.Background()) {
		t.Fatal("want pending=true while CM lags")
	}

	// CM reflects committed set -> not pending
	h = mk("denikson/BepInExPack_Valheim/5.4.2202\nValheimModding/Jotunn/2.25.0\n")
	h.SetPending(committed)
	if h.PendingActive(context.Background()) {
		t.Fatal("want pending=false once CM matches")
	}
	if h.pendSet != nil {
		t.Fatal("pending should self-clear when satisfied")
	}

	// TTL lapse clears stuck pending
	h = mk("ValheimModding/Jotunn/2.24.3\n")
	h.SetPending(committed)
	h.pendMu.Lock()
	h.pendAt = time.Now().Add(-2 * PendingTTL)
	h.pendMu.Unlock()
	if h.PendingActive(context.Background()) {
		t.Fatal("want pending=false after TTL")
	}
}

func TestModUpdates(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "valheim-mods", Namespace: "valheim"},
		Data: map[string]string{"mods.txt": "# server\n" +
			"denikson/BepInExPack_Valheim/5.4.2202\n" +
			"ValheimModding/Jotunn/2.24.3\n"},
	}
	cs := fake.NewSimpleClientset(cm)

	ts := thunderstore.New("https://example.invalid")
	ts.Preload([]thunderstore.SearchResult{
		{Owner: "denikson", Name: "BepInExPack_Valheim", Version: "5.4.2202"},
		{Owner: "ValheimModding", Name: "Jotunn", Version: "2.25.0"},
	}, time.Now())

	h := New(Config{
		Cfg: &config.Config{},
		K8s: k8s.NewWithClientset(cs, "valheim", "valheim"),
		TS:  ts,
	})

	ups := h.ModUpdates(context.Background())
	if len(ups) != 1 {
		t.Fatalf("want 1 update, got %d: %+v", len(ups), ups)
	}
	u := ups[0]
	if u.Key != "ValheimModding/Jotunn" || u.Current != "2.24.3" || u.Latest != "2.25.0" {
		t.Fatalf("unexpected update: %+v", u)
	}
	if u.Token != "ValheimModding_Jotunn" {
		t.Fatalf("token = %q", u.Token)
	}
}

func TestModpackExport(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cm := func(name string, data map[string]string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "valheim"},
			Data:       data,
		}
	}
	cs := fake.NewSimpleClientset(
		cm("valheim-mods", map[string]string{"mods.txt": "# my server\ndenikson/BepInExPack_Valheim/5.4.2202\nValheimModding/Jotunn/2.24.3\n"}),
		cm("valheim-mod-configs", map[string]string{"com.jotunn.jotunn.cfg": "[General]\nEnabled = true\n"}),
	)

	h := New(Config{
		Cfg:   &config.Config{},
		Store: st,
		K8s:   k8s.NewWithClientset(cs, "valheim", "valheim"),
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

func TestModIndexConversion(t *testing.T) {
	res := []thunderstore.SearchResult{
		{Owner: "author", Name: "coolmod", Version: "1.0.0", Description: "A cool mod"},
	}
	rows := ResultsToRows(res)
	if len(rows) != 1 || rows[0].Name != "coolmod" || rows[0].Namespace != "author" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	back := RowsToResults(rows)
	if len(back) != 1 || back[0].Name != "coolmod" || back[0].Owner != "author" {
		t.Fatalf("unexpected back conversion: %+v", back)
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
