package domain

import "testing"

func TestParseModSort(t *testing.T) {
	cases := map[string]ModSort{
		"relevance": ModSortRelevance,
		"DOWNLOADS": ModSortDownloads,
		" updated ": ModSortUpdated,
		"name":      ModSortName,
		"":          "",
		"popular":   "",
	}
	for in, want := range cases {
		if got := ParseModSort(in); got != want {
			t.Errorf("ParseModSort(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModQueryWithDefaults(t *testing.T) {
	// Without text there is nothing to rank by, so relevance is not an order.
	for _, sort := range []ModSort{"", ModSortRelevance} {
		q := ModQuery{Sort: sort}.WithDefaults()
		if q.Sort != ModSortDownloads {
			t.Errorf("empty text with sort %q = %q, want downloads", sort, q.Sort)
		}
		if q.Limit != DefaultModSearchLimit {
			t.Errorf("limit = %d, want %d", q.Limit, DefaultModSearchLimit)
		}
	}

	q := ModQuery{Text: "  jotunn  "}.WithDefaults()
	if q.Text != "jotunn" {
		t.Errorf("text = %q, want trimmed", q.Text)
	}
	if q.Sort != ModSortRelevance {
		t.Errorf("sort = %q, want relevance", q.Sort)
	}

	if q := (ModQuery{Text: "x", Sort: ModSortName, Limit: 5}).WithDefaults(); q.Sort != ModSortName || q.Limit != 5 {
		t.Errorf("explicit sort and limit were overwritten: %+v", q)
	}
}

func TestModSearchPageMore(t *testing.T) {
	page := ModSearchPage{Results: make([]ModSearchResult, 2), Total: 2}
	if page.More() {
		t.Error("a complete page reports more")
	}
	page.Total = 3
	if !page.More() {
		t.Error("a truncated page reports no more")
	}
}
