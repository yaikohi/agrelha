package overview_test

import (
	"context"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/capacity"
	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"agrelha/internal/infra/manifests"
	"agrelha/internal/infra/store"
	"agrelha/internal/ports"
	"agrelha/internal/web/handlers/overview"
	"agrelha/internal/web/shared"
)

type memStore struct{ docs map[string]ports.Document }

func (m *memStore) Get(_ context.Context, p string) (ports.Document, error) {
	return m.docs[p], nil
}
func (m *memStore) Put(_ context.Context, p string, d ports.Document, _ string) error {
	m.docs[p] = d
	return nil
}
func (m *memStore) Patch(context.Context, string, string, func(*ports.Document) (bool, error)) (bool, error) {
	return false, nil
}
func (m *memStore) Delete(_ context.Context, p string, _ string) error {
	delete(m.docs, p)
	return nil
}
func (m *memStore) PutTree(_ context.Context, dir string, docs map[string]ports.Document, _ string) error {
	for k, v := range docs {
		m.docs[dir+"/"+k] = v
	}
	return nil
}

type rig struct {
	app   *fiber.App
	mgr   *instances.InstanceManager
	cap   *capacity.Service
	state *memStore
}

func newRig(t *testing.T) *rig {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "overview.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	state := &memStore{docs: map[string]ports.Document{}}
	mgr := instances.NewInstanceManager(
		store.NewInstanceRepo(st), state, nil, 64, 4, 4,
		"manifests/minecraft-modded", "192.168.20.224",
		manifests.New("n=1", "minecraft-modded"), "minecraft-modded",
		instances.WithModsReader(func(context.Context, int) ([]string, error) { return nil, nil }),
	)
	cap := capacity.New(st, st, capacity.WithGlobals(st))
	if err := cap.Seed(context.Background(), domain.GameMinecraft, domain.GameSettings{
		TotalBudgetGiB: 64, MaxInstances: 4, MaxRunning: 4,
		Ceiling: domain.Resources{MemLimitGiB: 16},
	}); err != nil {
		t.Fatal(err)
	}

	h := overview.New(overview.Config{
		InstanceManagers: map[domain.GameID]*instances.InstanceManager{domain.GameMinecraft: mgr},
		Capacity:         cap,
	})

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(shared.PrincipalKey, domain.NewPrincipal(domain.Identity{
			Subject: "admin", Email: "admin@example.com",
			Roles: []domain.Role{domain.RoleAdmin},
		}))
		return c.Next()
	})
	h.Register(app)
	return &rig{app: app, mgr: mgr, cap: cap, state: state}
}

func (r *rig) post(t *testing.T, path string, form url.Values) {
	t.Helper()
	req := httptest.NewRequest(fiber.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := r.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("POST %s status = %d, want 303", path, resp.StatusCode)
	}
}

func (r *rig) seed(t *testing.T) domain.Instance {
	t.Helper()
	inst, err := r.mgr.CreateInstance(context.Background(), domain.Instance{
		Name: "World", Source: domain.SourceModlist, Tier: domain.TierMedium,
		Minecraft: &domain.MinecraftConfig{Loader: domain.LoaderNeoForge, MCVersion: "1.21.1"},
	}, domain.ModList{})
	if err != nil {
		t.Fatal(err)
	}
	return *inst
}

func (r *rig) resources(t *testing.T, num int) domain.Resources {
	t.Helper()
	got, err := r.mgr.GetInstance(context.Background(), num)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	return got.EffectiveResources(domain.MinecraftProfile)
}

func TestOverviewPageRenders(t *testing.T) {
	r := newRig(t)
	r.seed(t)

	resp, err := r.app.Test(httptest.NewRequest(fiber.MethodGet, "/overview", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("GET /overview = %d, want 200", resp.StatusCode)
	}
}

func TestSaveResourcesResizesTheWorld(t *testing.T) {
	r := newRig(t)
	inst := r.seed(t)

	r.post(t, "/overview/resources", url.Values{
		"target": {"minecraft:1"}, "memreq": {"12"}, "memlimit": {"16"},
		"cpureq": {"2000"}, "cpulimit": {"0"},
	})

	got := r.resources(t, inst.Number)
	if got.MemRequestGiB != 12 || got.MemLimitGiB != 16 {
		t.Errorf("resources = %d/%d GiB, want 12/16", got.MemRequestGiB, got.MemLimitGiB)
	}
	dep := string(r.state.docs["manifests/minecraft-modded/instance-01/deployment.yaml"].Raw)
	if !strings.Contains(dep, "memory: 12Gi") {
		t.Error("the resize did not reach the rendered manifest")
	}
}

// The central promise of tier-as-preset: editing a tier changes what NEW worlds
// get, and moves nothing that already exists.
func TestSavingATierDoesNotResizeExistingWorlds(t *testing.T) {
	r := newRig(t)
	inst := r.seed(t)
	before := r.resources(t, inst.Number)

	r.post(t, "/overview/tier", url.Values{
		"game": {"minecraft"}, "key": {"medium"},
		"memreq": {"20"}, "memlimit": {"24"}, "cpureq": {"2000"}, "heap": {"12"},
	})

	after := r.resources(t, inst.Number)
	if after != before {
		t.Errorf("editing a tier moved an existing world: %+v -> %+v", before, after)
	}

	tier, err := r.cap.Tier(context.Background(), domain.GameMinecraft, "medium")
	if err != nil || tier == nil {
		t.Fatal(err)
	}
	if tier.Resources.MemLimitGiB != 24 || tier.HeapInitGiB != 12 {
		t.Errorf("tier not saved: %+v heap=%d", tier.Resources, tier.HeapInitGiB)
	}
}

func TestApplyTierRestoresADriftedWorld(t *testing.T) {
	r := newRig(t)
	inst := r.seed(t)

	r.post(t, "/overview/resources", url.Values{
		"target": {"minecraft:1"}, "memreq": {"14"}, "memlimit": {"15"},
		"cpureq": {"2000"}, "cpulimit": {"0"},
	})
	if got := r.resources(t, inst.Number); got.MemLimitGiB != 15 {
		t.Fatalf("setup failed, limit = %d", got.MemLimitGiB)
	}

	r.post(t, "/overview/apply-tier", url.Values{"target": {"minecraft:1"}})

	got := r.resources(t, inst.Number)
	if got.MemRequestGiB != 8 || got.MemLimitGiB != 10 {
		t.Errorf("apply-tier left %d/%d GiB, want the medium tier's 8/10", got.MemRequestGiB, got.MemLimitGiB)
	}
}

func TestSaveAllowancePersists(t *testing.T) {
	r := newRig(t)
	r.post(t, "/overview/allowance", url.Values{"allowance": {"3"}})
	if n := r.cap.FreeCreations(context.Background()); n != 3 {
		t.Errorf("allowance = %d, want 3", n)
	}
}

func TestUnknownGameIsRejected(t *testing.T) {
	r := newRig(t)
	r.post(t, "/overview/settings", url.Values{"game": {"pinball"}, "budget": {"8"}})
	// Redirects with a flash rather than 500ing; nothing is stored for a game
	// that is not registered.
	if s, err := r.cap.Settings(context.Background(), domain.GameID("pinball")); err != nil || s.TotalBudgetGiB != 0 {
		t.Errorf("settings were stored for an unregistered game: %+v", s)
	}
}
