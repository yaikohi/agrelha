package store

import (
	"database/sql"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

var _ ports.ReadmeCache = (*Store)(nil)

type ModIndexRow struct {
	FullName     string
	Namespace    string
	Name         string
	Owner        string
	Version      string
	Description  string
	Icon         string
	PackageURL   string
	Downloads    int64
	IsDeprecated bool
	UpdatedAt    time.Time
}

func (s *Store) SaveModIndex(rows []ModIndexRow, fetchedAt time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM mod_index`); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO mod_index
		(full_name,namespace,name,owner,version,description,icon,package_url,downloads,is_deprecated,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		dep := 0
		if r.IsDeprecated {
			dep = 1
		}
		if _, err := stmt.Exec(r.FullName, r.Namespace, r.Name, r.Owner, r.Version,
			r.Description, r.Icon, r.PackageURL, r.Downloads, dep, r.UpdatedAt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO meta(key,value) VALUES('mod_index_fetched_at',?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		fetchedAt.UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) LoadModIndex() ([]ModIndexRow, time.Time, error) {
	var fetchedAt time.Time
	var v string
	switch err := s.db.QueryRow(`SELECT value FROM meta WHERE key='mod_index_fetched_at'`).Scan(&v); err {
	case nil:
		fetchedAt, _ = time.Parse(time.RFC3339, v)
	case sql.ErrNoRows:
	default:
		return nil, time.Time{}, err
	}

	rows, err := s.db.Query(`SELECT full_name,namespace,name,owner,version,description,icon,package_url,downloads,is_deprecated,updated_at
		FROM mod_index ORDER BY downloads DESC`)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer rows.Close()

	var out []ModIndexRow
	for rows.Next() {
		var r ModIndexRow
		var dep int
		var updated sql.NullTime
		if err := rows.Scan(&r.FullName, &r.Namespace, &r.Name, &r.Owner, &r.Version,
			&r.Description, &r.Icon, &r.PackageURL, &r.Downloads, &dep, &updated); err != nil {
			return nil, time.Time{}, err
		}
		r.IsDeprecated = dep != 0
		if updated.Valid {
			r.UpdatedAt = updated.Time
		}
		out = append(out, r)
	}
	return out, fetchedAt, rows.Err()
}

func (s *Store) GetReadme(fullName, version string) (markdown string, hit bool, err error) {
	err = s.db.QueryRow(`SELECT markdown FROM mod_readme WHERE full_name=? AND version=?`,
		fullName, version).Scan(&markdown)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return markdown, true, nil
}

func (s *Store) PutReadme(fullName, version, markdown string) error {
	_, err := s.db.Exec(`INSERT INTO mod_readme(full_name,version,markdown) VALUES(?,?,?)
		ON CONFLICT(full_name,version) DO UPDATE SET markdown=excluded.markdown, fetched_at=CURRENT_TIMESTAMP`,
		fullName, version, markdown)
	return err
}

// RowsToResults converts store ModIndexRows to domain ModSearchResults.
func RowsToResults(rows []ModIndexRow) []domain.ModSearchResult {
	out := make([]domain.ModSearchResult, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.ModSearchResult{
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

// ResultsToRows converts domain ModSearchResults to store ModIndexRows.
func ResultsToRows(idx []domain.ModSearchResult) []ModIndexRow {
	out := make([]ModIndexRow, 0, len(idx))
	for _, r := range idx {
		out = append(out, ModIndexRow{
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
