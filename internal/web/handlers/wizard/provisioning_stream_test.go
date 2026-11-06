package wizard

import (
	"context"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/app/instances"
	"agrelha/internal/domain"
	"agrelha/internal/infra/manifests"
	"agrelha/internal/infra/store"
)

// tracedApp reproduces otelfiber's context lifecycle: it hands the handler a
// cancellable context through UserContext and cancels it in a defer as the
// middleware unwinds - which happens before fasthttp runs the body stream
// writer. Any streaming handler that holds on to that context is already
// cancelled by the time it writes its first byte.
func tracedApp(t *testing.T) (*fiber.App, *instances.InstanceManager) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "wizard_traced.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	instMgr := instances.NewInstanceManager(
		store.NewInstanceRepo(st), &mockStore{}, nil, 32, 4, 2,
		"manifests/minecraft-modded", "192.168.20.224",
		manifests.New("ykhi.xyz/gameserver=true", "minecraft-modded"), "minecraft-modded",
		instances.WithGameID(domain.GameMinecraft),
	)

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		c.SetUserContext(ctx)
		return c.Next()
	})
	New(Config{MCInstances: instMgr}).Register(app)
	return app, instMgr
}

func TestMCProvisioningStreamEmitsUnderATracedRequest(t *testing.T) {
	app, instMgr := tracedApp(t)
	if err := instMgr.SaveInstance(domain.Instance{
		GameID: domain.GameMinecraft, Number: 11,
		Name: "Traced", State: domain.StateProvisioning,
	}); err != nil {
		t.Fatalf("save instance: %v", err)
	}

	origSleep := sleep
	t.Cleanup(func() { sleep = origSleep })
	sleep = func(time.Duration) {}

	resp, err := app.Test(httptest.NewRequest("GET", "/api/minecraft/provisioning/11/stream", nil), 5000)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)

	if !strings.Contains(string(body), "datastar-patch-signals") {
		t.Fatalf(`the provisioning stream emitted nothing under a traced request.

The page keeps whatever its data-signals attribute set up (phase 'syncing'), so a
world that is already up still shows "applying compose" and never reaches its
final step. Body was: %q`, string(body))
	}
	if !strings.Contains(string(body), `"phase"`) {
		t.Errorf("no phase signal in stream body: %q", string(body))
	}
}
