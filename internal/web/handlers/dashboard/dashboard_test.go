package dashboard

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"

	"github.com/gofiber/fiber/v2"
)

func TestDashboardPage(t *testing.T) {
	h := New(Config{
		GameNodeName:   "game-01",
		ValheimAddress: "192.168.20.224:2456",
	})

	app := fiber.New()
	h.Register(app)

	// 1. Visit dashboard as guest
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "game-01") {
		t.Errorf("expected node name game-01 in dashboard body")
	}
}

func TestTileSignals(t *testing.T) {
	h := New(Config{
		BackupInfo: func() (BackupSummary, bool) {
			return BackupSummary{
				Count:      3,
				TotalSize:  1024 * 1024 * 50,
				LatestSize: 1024 * 1024 * 20,
				LatestAt:   time.Now().Add(-5 * time.Minute),
			}, true
		},
	})

	sig := h.TileSignals(context.Background())
	if sig["backupinfo"] == "" {
		t.Errorf("expected non-empty backupinfo signal")
	}
	if sig["backup"] == "—" {
		t.Errorf("expected human ago for backup, got '—'")
	}
}

type fakeGameEngine struct {
	telemetry domain.GameTelemetry
}

func (f *fakeGameEngine) ID() domain.GameID                  { return "mock" }
func (f *fakeGameEngine) Display() domain.Display            { return domain.Display{Name: "Mock"} }
func (f *fakeGameEngine) Providers() []ports.ContentProvider { return nil }
func (f *fakeGameEngine) ResolveContent(ctx context.Context, inst domain.Instance) (domain.ContentSet, error) {
	return domain.ContentSet{}, nil
}
func (f *fakeGameEngine) ExportClientBundle(ctx context.Context, inst domain.Instance) (domain.Bundle, error) {
	return domain.Bundle{}, nil
}
func (f *fakeGameEngine) RuntimeSpec(inst domain.Instance) domain.RuntimeSpec {
	return domain.RuntimeSpec{}
}
func (f *fakeGameEngine) Telemetry(ctx context.Context) (domain.GameTelemetry, error) {
	return f.telemetry, nil
}
func (f *fakeGameEngine) AdmissionModel() domain.AdmissionModel { return "" }
func (f *fakeGameEngine) OperatorIDKind() domain.OperatorIDKind { return "" }

func TestTileSignalsWithGameEngines(t *testing.T) {
	valheim := &fakeGameEngine{
		telemetry: domain.GameTelemetry{
			State:        "Up",
			Online:       true,
			Players:      5,
			PlayersKnown: true,
			Uptime:       "3h 20m",
			CPU:          "250m",
			Memory:       "1500 Mi",
		},
	}
	minecraft := &fakeGameEngine{
		telemetry: domain.GameTelemetry{
			State:        "Up",
			Online:       true,
			Players:      2,
			PlayersKnown: true,
			Uptime:       "1d 2h",
			CPU:          "800m",
			Memory:       "6144 Mi",
			Loader:       "Fabric",
			PackName:     "Cobblemon Official",
		},
	}

	h := New(Config{
		ValheimGame:   valheim,
		MinecraftGame: minecraft,
	})

	sig := h.TileSignals(context.Background())

	if sig["state"] != "Up" || sig["online"] != true {
		t.Errorf("Valheim state/online unexpected: state=%v, online=%v", sig["state"], sig["online"])
	}
	if sig["players"] != 5 {
		t.Errorf("Valheim players = %v, want 5", sig["players"])
	}
	if sig["uptime"] != "3h 20m" || sig["cpu"] != "250m" || sig["mem"] != "1500 Mi" {
		t.Errorf("Valheim metrics unexpected: %v, %v, %v", sig["uptime"], sig["cpu"], sig["mem"])
	}

	if sig["mc_state"] != "Up" || sig["mc_players"] != 2 {
		t.Errorf("MC state/players unexpected: state=%v, players=%v", sig["mc_state"], sig["mc_players"])
	}
	if sig["mc_loader"] != "Fabric" || sig["mc_pack"] != "Cobblemon Official" {
		t.Errorf("MC loader/pack unexpected: loader=%v, pack=%v", sig["mc_loader"], sig["mc_pack"])
	}
	if sig["mc_uptime"] != "1d 2h" || sig["mc_cpu"] != "800m" || sig["mc_mem"] != "6144 Mi" {
		t.Errorf("MC metrics unexpected: %v, %v, %v", sig["mc_uptime"], sig["mc_cpu"], sig["mc_mem"])
	}
}
