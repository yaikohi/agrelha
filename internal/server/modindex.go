package server

import (
	"agrelha/internal/infra/content/thunderstore"
	"agrelha/internal/infra/store"
	contenthttp "agrelha/internal/web/handlers/content"
)

func rowsToResults(rows []store.ModIndexRow) []thunderstore.SearchResult {
	return contenthttp.RowsToResults(rows)
}

func resultsToRows(idx []thunderstore.SearchResult) []store.ModIndexRow {
	return contenthttp.ResultsToRows(idx)
}
