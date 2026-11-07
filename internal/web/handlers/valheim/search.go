package valheim

import (
	"fmt"
	"html"

	"agrelha/internal/domain"
)

var sortLabels = map[domain.ModSort]string{
	domain.ModSortRelevance: "best match",
	domain.ModSortDownloads: "downloads",
	domain.ModSortUpdated:   "last updated",
	domain.ModSortName:      "name",
}

// searchHeading states what was searched, how many matched in all, and in what
// order. The count is the match total, never the page size.
func searchHeading(q domain.ModQuery, page domain.ModSearchPage) (heading, note string) {
	switch {
	case q.Text == "":
		heading = "Popular on Thunderstore"
	case page.Total == 1:
		heading = fmt.Sprintf("1 result for %q", q.Text)
	default:
		heading = fmt.Sprintf("%d results for %q", page.Total, q.Text)
	}
	note = "sorted by " + sortLabels[q.Sort]
	if page.More() {
		note = fmt.Sprintf("showing %d · %s", len(page.Results), note)
	}
	if q.Deprecated {
		note += " · deprecated included"
	}
	return heading, note
}

// emptySearchText says which filter emptied the list, so the operator knows
// whether to change the words or the filters.
func emptySearchText(q domain.ModQuery) string {
	if q.Text == "" {
		return "No Thunderstore mods match that search."
	}
	if q.Deprecated {
		return fmt.Sprintf("No Thunderstore mods match %q.", q.Text)
	}
	return fmt.Sprintf("No Thunderstore mods match %q. Deprecated mods are hidden; tick Show deprecated to include them.", q.Text)
}

func emptySearch(q domain.ModQuery) string {
	return fmt.Sprintf(`<p class="px-6 py-10 font-pixel text-[11px] text-ink-faint">%s</p>`, html.EscapeString(emptySearchText(q)))
}
