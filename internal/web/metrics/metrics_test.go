package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler(t *testing.T) {
	h := Handler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	DatastarRequests.Inc()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected HTTP 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "agrelha_datastar_requests_total") {
		t.Errorf("expected metrics output to contain agrelha_datastar_requests_total")
	}
}
