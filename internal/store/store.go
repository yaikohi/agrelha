// Package store is agrelha's operational memory: an embedded SQLite DB (modernc,
// pure-Go, no cgo) holding the player roster/sessions, imperative-action audit,
// Thunderstore metadata cache, and the server-event timeline. Single writer.
package store

import (
	"database/sql"
	"fmt"
	"strings"

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
		sessions     INTEGER NOT NULL DEFAULT 0,
		online       INTEGER NOT NULL DEFAULT 0,
		online_since TIMESTAMP
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
	CREATE TABLE IF NOT EXISTS mod_index (
		full_name     TEXT PRIMARY KEY,
		namespace     TEXT,
		name          TEXT,
		owner         TEXT,
		version       TEXT,
		description   TEXT,
		icon          TEXT,
		package_url   TEXT,
		downloads     INTEGER NOT NULL DEFAULT 0,
		is_deprecated INTEGER NOT NULL DEFAULT 0,
		updated_at    TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS mod_readme (
		full_name  TEXT NOT NULL,
		version    TEXT NOT NULL,
		markdown   TEXT,
		fetched_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (full_name, version)
	);
	CREATE TABLE IF NOT EXISTS mc_slots (
		slot        TEXT PRIMARY KEY,
		type        TEXT NOT NULL,          -- AUTO_CURSEFORGE|NEOFORGE|FABRIC
		pack        TEXT,
		mc_version  TEXT,
		cf_page_url TEXT,
		created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		last_used   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS meta (
		key   TEXT PRIMARY KEY,
		value TEXT
	);
	`)
	if err != nil {
		return err
	}
	for _, stmt := range []string{
		`ALTER TABLE players ADD COLUMN online INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE players ADD COLUMN online_since TIMESTAMP`,
	} {
		if _, err := s.db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return nil
}
