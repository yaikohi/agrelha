package web_test

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/web"
	dashboardhttp "agrelha/internal/web/handlers/dashboard"
)

func TestRoutes_HealthzAndMetrics(t *testing.T) {
	app := web.New(web.ServerConfig{})

	req := httptest.NewRequest(fiber.MethodGet, "/healthz", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("GET /healthz status = %d, want 200", resp.StatusCode)
	}

	reqMetrics := httptest.NewRequest(fiber.MethodGet, "/metrics", nil)
	respMetrics, err := app.Test(reqMetrics)
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	if respMetrics.StatusCode != fiber.StatusOK {
		t.Errorf("GET /metrics status = %d, want 200", respMetrics.StatusCode)
	}
}

func TestRoutes_MountsDashboard(t *testing.T) {
	dh := dashboardhttp.New(dashboardhttp.Config{})
	app := web.New(web.ServerConfig{
		Dashboard: dh,
	})

	req := httptest.NewRequest(fiber.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("GET / status = %d, want 200", resp.StatusCode)
	}
}
