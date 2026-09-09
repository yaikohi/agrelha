package dashboard

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agrelha/internal/infra/backups"
	"agrelha/internal/infra/store"
	"agrelha/internal/platform/config"
	"agrelha/internal/web/pages"

	"github.com/gofiber/fiber/v2"
)

func TestDashboardPage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "dash.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cfg := &config.Config{
		GameNodeName:   "game-01",
		ValheimAddress: "192.168.20.224:2456",
	}

	h := New(Config{
		Cfg:   cfg,
		Store: st,
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
	dbPath := filepath.Join(t.TempDir(), "signals.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	h := New(Config{
		Store: st,
		BackupInfo: func() (backups.Info, bool) {
			return backups.Info{
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

func TestUpdatesSignature(t *testing.T) {
	ups := []pages.ModUpdate{
		{Key: "mod1", Latest: "1.0.1"},
		{Key: "mod2", Latest: "2.3.0"},
	}
	sig := updatesSignature(ups)
	want := "mod1@1.0.1;mod2@2.3.0;"
	if sig != want {
		t.Errorf("updatesSignature = %q, want %q", sig, want)
	}
}
