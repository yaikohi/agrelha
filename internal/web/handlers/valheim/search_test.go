package valheim

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"agrelha/internal/domain"
)

func TestSearchFiltersReachTheCatalogue(t *testing.T) {
	h, st, _, _, _, mockTS, _ := setupTestValheimHandlerFull(t)
	defer st.Close()

	app := fiber.New()
	h.Register(app)

	for _, path := range []string{
		"/api/valheim/1/mods/search?q=jotunn&sort=updated&deprecated=true",
		"/api/valheim/wizard/mods/search?q=jotunn&sort=updated&deprecated=true",
	} {
		mockTS.lastQuery = domain.ModQuery{}
		if _, err := app.Test(httptest.NewRequest("GET", path, nil)); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		got := mockTS.lastQuery
		if got.Text != "jotunn" || got.Sort != domain.ModSortUpdated || !got.Deprecated {
			t.Errorf("%s passed %+v", path, got)
		}
	}
}

func TestSearchResultsCarryTheFiltersIntoLoadMore(t *testing.T) {
	h, st, _, _, _, mockTS, _ := setupTestValheimHandlerFull(t)
	defer st.Close()

	mockTS.results = append(mockTS.results, domain.ModSearchResult{Owner: "Azumatt", Name: "AzuAntiCheat"})

	app := fiber.New()
	h.Register(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/api/valheim/1/mods/search?q=a&limit=1", nil))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	// The Load more button reads the live signals, so a filter set after this
	// render still applies to the next page.
	for _, want := range []string{"Load more", "q: $modQuery", "sort: $modSort", "deprecated: $modDeprecated", "limit: 25"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("load more script missing %q in: %s", want, string(body))
		}
	}
}

func TestSearchHeadingStatesTotalAndSort(t *testing.T) {
	cases := []struct {
		name  string
		query domain.ModQuery
		page  domain.ModSearchPage
		want  []string
	}{
		{
			name:  "no text is popularity",
			query: domain.ModQuery{}.WithDefaults(),
			page:  domain.ModSearchPage{Results: make([]domain.ModSearchResult, 2), Total: 2},
			want:  []string{"Popular on Thunderstore", "sorted by downloads"},
		},
		{
			name:  "truncated page states both numbers",
			query: domain.ModQuery{Text: "jotunn"}.WithDefaults(),
			page:  domain.ModSearchPage{Results: make([]domain.ModSearchResult, 24), Total: 61},
			want:  []string{`61 results for "jotunn"`, "showing 24", "sorted by best match"},
		},
		{
			name:  "deprecated is disclosed",
			query: domain.ModQuery{Text: "x", Deprecated: true}.WithDefaults(),
			page:  domain.ModSearchPage{Results: make([]domain.ModSearchResult, 1), Total: 1},
			want:  []string{`1 result for "x"`, "deprecated included"},
		},
	}
	for _, tc := range cases {
		heading, note := searchHeading(tc.query, tc.page)
		line := heading + " · " + note
		for _, want := range tc.want {
			if !strings.Contains(line, want) {
				t.Errorf("%s: %q missing %q", tc.name, line, want)
			}
		}
	}
}

func TestEmptySearchNamesTheDeprecatedFilter(t *testing.T) {
	if msg := emptySearchText(domain.ModQuery{Text: "jotunn"}); !strings.Contains(msg, "Show deprecated") {
		t.Errorf("hidden deprecated not mentioned: %q", msg)
	}
	if msg := emptySearchText(domain.ModQuery{Text: "jotunn", Deprecated: true}); strings.Contains(msg, "Show deprecated") {
		t.Errorf("deprecated filter mentioned when it was off: %q", msg)
	}
}
