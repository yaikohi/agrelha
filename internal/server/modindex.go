package server

import (
	"agrelha/internal/store"
	"agrelha/internal/thunderstore"
)

func rowsToResults(rows []store.ModIndexRow) []thunderstore.SearchResult {
	out := make([]thunderstore.SearchResult, 0, len(rows))
	for _, r := range rows {
		out = append(out, thunderstore.SearchResult{
			Owner:        r.Owner,
			Name:         r.Name,
			FullURL:      r.PackageURL,
			Description:  r.Description,
			Icon:         r.Icon,
			Version:      r.Version,
			Downloads:    r.Downloads,
			IsDeprecated: r.IsDeprecated,
			UpdatedAt:    r.UpdatedAt,
		})
	}
	return out
}

func resultsToRows(idx []thunderstore.SearchResult) []store.ModIndexRow {
	out := make([]store.ModIndexRow, 0, len(idx))
	for _, r := range idx {
		out = append(out, store.ModIndexRow{
			FullName:     r.FullName(),
			Namespace:    r.Owner,
			Name:         r.Name,
			Owner:        r.Owner,
			Version:      r.Version,
			Description:  r.Description,
			Icon:         r.Icon,
			PackageURL:   r.FullURL,
			Downloads:    r.Downloads,
			IsDeprecated: r.IsDeprecated,
			UpdatedAt:    r.UpdatedAt,
		})
	}
	return out
}
