package store

import (
	"database/sql"
	"strings"
	"time"

	"agrelha/internal/domain"
)

const restoreSep = "\n"

// SaveRestorePoint records the mod list an Instance had before an update, plus
// the list the update wrote. Keeping both is what lets the undo refuse when
// something else has changed the list in the meantime. One point per Instance:
// a new update replaces the old way back.
func (s *Store) SaveRestorePoint(game domain.GameID, number int, previous, applied []string) error {
	_, err := s.db.Exec(`INSERT INTO mod_restore_points (game_id, number, at, previous, applied)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(game_id, number) DO UPDATE SET
			at=excluded.at, previous=excluded.previous, applied=excluded.applied`,
		string(game), number, time.Now().UTC(),
		strings.Join(previous, restoreSep), strings.Join(applied, restoreSep))
	return err
}

// RestorePoint returns the way back for an Instance, or nil when there is none.
func (s *Store) RestorePoint(game domain.GameID, number int) (*domain.ModRestorePoint, error) {
	var prev, applied string
	var at time.Time
	err := s.db.QueryRow(`SELECT at, previous, applied FROM mod_restore_points
		WHERE game_id = ? AND number = ?`, string(game), number).Scan(&at, &prev, &applied)
	switch {
	case err == sql.ErrNoRows:
		return nil, nil
	case err != nil:
		return nil, err
	}
	return &domain.ModRestorePoint{
		At:       at,
		Previous: splitRestore(prev),
		Applied:  splitRestore(applied),
	}, nil
}

// ClearRestorePoint drops an Instance's way back, after it is used or invalidated.
func (s *Store) ClearRestorePoint(game domain.GameID, number int) error {
	_, err := s.db.Exec(`DELETE FROM mod_restore_points WHERE game_id = ? AND number = ?`,
		string(game), number)
	return err
}

func splitRestore(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, restoreSep)
}
