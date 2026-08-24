// Package store is agrelha's operational memory: an embedded SQLite DB (modernc,
// pure-Go, no cgo) holding the player roster/sessions, imperative-action audit,
// Thunderstore metadata cache, and the server-event timeline. Single writer.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS players (
		steam_id     TEXT PRIMARY KEY,
		character    TEXT,
		first_seen   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		last_seen    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		sessions     INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS audit (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		actor      TEXT NOT NULL,          -- OIDC email
		action     TEXT NOT NULL,          -- restart|stop|start|mod-install|admin-grant|...
		detail     TEXT
	);
	CREATE TABLE IF NOT EXISTS events (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		kind       TEXT NOT NULL,          -- join|leave|restart|update|backup|crash
		detail     TEXT
	);
	CREATE TABLE IF NOT EXISTS mod_cache (
		full_name  TEXT PRIMARY KEY,       -- namespace/name
		latest     TEXT,
		deps_json  TEXT,
		icon_url   TEXT,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`)
	return err
}

// TODO(step③): UpsertPlayer, RecordAudit, RecordEvent, mod-cache upserts/reads.
