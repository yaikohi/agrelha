package shared

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/domain"
)

func parsed(t *testing.T, req *http.Request) domain.ModQuery {
	t.Helper()
	app := fiber.New()
	var got domain.ModQuery
	app.All("/s", func(c *fiber.Ctx) error {
		got = ParseModQuery(c)
		return c.SendString("ok")
	})
	if _, err := app.Test(req); err != nil {
		t.Fatalf("request: %v", err)
	}
	return got
}

func TestParseModQueryFromJSONBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/s", strings.NewReader(`{"q":"jotunn","sort":"updated","deprecated":true,"limit":48}`))
	req.Header.Set("Content-Type", "application/json")

	q := parsed(t, req)
	if q.Text != "jotunn" || q.Sort != domain.ModSortUpdated || !q.Deprecated || q.Limit != 48 {
		t.Errorf("parsed = %+v", q)
	}
}

func TestParseModQueryAliasesAndQueryString(t *testing.T) {
	req := httptest.NewRequest("POST", "/s", strings.NewReader(`{"wizardModSearch":"mining"}`))
	req.Header.Set("Content-Type", "application/json")
	if q := parsed(t, req); q.Text != "mining" {
		t.Errorf("wizard alias ignored: %+v", q)
	}

	q := parsed(t, httptest.NewRequest("GET", "/s?q=mining&sort=name&deprecated=true&limit=10", nil))
	if q.Text != "mining" || q.Sort != domain.ModSortName || !q.Deprecated || q.Limit != 10 {
		t.Errorf("query string = %+v", q)
	}
}

func TestParseModQueryDefaults(t *testing.T) {
	q := parsed(t, httptest.NewRequest("GET", "/s", nil))
	if q.Text != "" || q.Sort != domain.ModSortDownloads || q.Deprecated || q.Limit != domain.DefaultModSearchLimit {
		t.Errorf("defaults = %+v", q)
	}

	// An unknown sort falls back rather than reaching a catalogue adapter.
	if q := parsed(t, httptest.NewRequest("GET", "/s?q=x&sort=popular", nil)); q.Sort != domain.ModSortRelevance {
		t.Errorf("unknown sort = %q, want relevance", q.Sort)
	}
}
