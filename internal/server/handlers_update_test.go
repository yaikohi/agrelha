package server

import (
	"context"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"agrelha/internal/infra/content/thunderstore"
	"agrelha/internal/infra/kube"
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
		if got := versionNewer(c.a, c.b); got != c.want {
			t.Errorf("versionNewer(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestPendingActive(t *testing.T) {
	mk := func(modsTxt string) *FiberServer {
		cs := fake.NewSimpleClientset(&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "valheim-mods", Namespace: "valheim"},
			Data:       map[string]string{"mods.txt": modsTxt},
		})
		return &FiberServer{cfg: &config.Config{}, k8s: k8s.NewWithClientset(cs, "valheim", "valheim")}
	}
	committed := []string{"ValheimModding/Jotunn/2.25.0", "denikson/BepInExPack_Valheim/5.4.2202"}

	// CM still holds the old version -> pending.
	s := mk("denikson/BepInExPack_Valheim/5.4.2202\nValheimModding/Jotunn/2.24.3\n")
	s.setPending(committed)
	if !s.pendingActive(context.Background()) {
		t.Fatal("want pending=true while CM lags")
	}

	// CM now reflects the committed set -> not pending (and self-clears).
	s = mk("denikson/BepInExPack_Valheim/5.4.2202\nValheimModding/Jotunn/2.25.0\n")
	s.setPending(committed)
	if s.pendingActive(context.Background()) {
		t.Fatal("want pending=false once CM matches")
	}
	if s.pendSet != nil {
		t.Fatal("pending should self-clear when satisfied")
	}

	// TTL lapse clears a stuck pending even if the CM never catches up.
	s = mk("ValheimModding/Jotunn/2.24.3\n")
	s.setPending(committed)
	s.pendMu.Lock()
	s.pendAt = time.Now().Add(-2 * pendingTTL)
	s.pendMu.Unlock()
	if s.pendingActive(context.Background()) {
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

	s := &FiberServer{
		App: fiber.New(),
		cfg: &config.Config{},
		k8s: k8s.NewWithClientset(cs, "valheim", "valheim"),
		ts:  ts,
	}

	ups := s.modUpdates(context.Background())
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
