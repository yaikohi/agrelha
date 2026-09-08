package server

import (
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/config"
	"agrelha/internal/store"
)

func TestValheimConsolePage(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	s := &FiberServer{App: fiber.New(), cfg: &config.Config{}, store: st}
	s.RegisterFiberRoutes()

	resp, err := s.App.Test(httptest.NewRequest(fiber.MethodGet, "/valheim", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	for _, want := range []string{`id="logs"`, "/sse/logs?server=valheim", "Console"} {
		if !strings.Contains(html, want) {
			t.Fatalf("page missing %q", want)
		}
	}
	if strings.Contains(html, "RCON command") {
		t.Fatal("Valheim has no RCON; a command bar must not be rendered")
	}
}
