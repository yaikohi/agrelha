package thunderstore

import (
	"context"
	"testing"
	"time"

	"agrelha/internal/domain"
)

func indexed() *Client {
	c := New("http://unused")
	c.Preload([]SearchResult{
		{Owner: "ValheimModding", Name: "Jotunn", Downloads: 10000, UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Owner: "Smoothbrain", Name: "Mining", Downloads: 5000, UpdatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
		{Owner: "Azumatt", Name: "AzuAntiCheat", Downloads: 900, Description: "Depends on Jotunn", UpdatedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		{Owner: "Dead", Name: "JotunnOld", Downloads: 999999, IsDeprecated: true, UpdatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
	}, time.Now())
	return c
}

func names(page domain.ModSearchPage) []string {
	out := make([]string, 0, len(page.Results))
	for _, r := range page.Results {
		out = append(out, r.Name)
	}
	return out
}

func TestSearchHidesDeprecatedUnlessAsked(t *testing.T) {
	ctx := context.Background()
	c := indexed()

	page, err := c.Search(ctx, domain.ModQuery{Text: "jotunn"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	for _, n := range names(page) {
		if n == "JotunnOld" {
			t.Fatalf("deprecated mod offered by default: %v", names(page))
		}
	}

	page, err = c.Search(ctx, domain.ModQuery{Text: "jotunn", Deprecated: true})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var found bool
	for _, n := range names(page) {
		found = found || n == "JotunnOld"
	}
	if !found {
		t.Fatalf("deprecated mod missing when asked for: %v", names(page))
	}
}

func TestSearchRanksNameBeforeOwnerAndDescription(t *testing.T) {
	page, err := indexed().Search(context.Background(), domain.ModQuery{Text: "jotunn"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	got := names(page)
	// Jotunn by name, then AzuAntiCheat which only mentions it in its description.
	want := []string{"Jotunn", "AzuAntiCheat"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("relevance order = %v, want %v", got, want)
	}
}

func TestSearchSorts(t *testing.T) {
	ctx := context.Background()
	c := indexed()

	cases := map[domain.ModSort][]string{
		domain.ModSortDownloads: {"Jotunn", "Mining", "AzuAntiCheat"},
		domain.ModSortUpdated:   {"Mining", "AzuAntiCheat", "Jotunn"},
		domain.ModSortName:      {"AzuAntiCheat", "Jotunn", "Mining"},
	}
	for sort, want := range cases {
		page, err := c.Search(ctx, domain.ModQuery{Sort: sort})
		if err != nil {
			t.Fatalf("search %s: %v", sort, err)
		}
		got := names(page)
		if len(got) != len(want) {
			t.Fatalf("sort %s = %v, want %v", sort, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("sort %s = %v, want %v", sort, got, want)
				break
			}
		}
	}
}

func TestSearchRanksTheWholeMatchSetBeforePaging(t *testing.T) {
	page, err := indexed().Search(context.Background(), domain.ModQuery{Sort: domain.ModSortName, Limit: 1})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Results) != 1 || page.Results[0].Name != "AzuAntiCheat" {
		t.Errorf("first page = %v, want AzuAntiCheat", names(page))
	}
	if page.Total != 3 || !page.More() {
		t.Errorf("total = %d, more = %v, want 3 and true", page.Total, page.More())
	}
}
