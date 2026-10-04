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
	dsn := path
	if !strings.Contains(dsn, "?") {
		dsn += "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	if err := s.backfillInstanceResources(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

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
	CREATE TABLE IF NOT EXISTS incidents (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		game_id       TEXT NOT NULL,
		number        INTEGER NOT NULL,
		at            TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		restart_count INTEGER NOT NULL DEFAULT 0,
		exit_code     INTEGER NOT NULL DEFAULT 0,
		reason        TEXT,
		oom_killed    INTEGER NOT NULL DEFAULT 0,
		log_tail      TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_incidents_instance ON incidents(game_id, number, at DESC);
	CREATE TABLE IF NOT EXISTS mod_restore_points (
		game_id   TEXT NOT NULL,
		number    INTEGER NOT NULL,
		at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		previous  TEXT NOT NULL,
		applied   TEXT NOT NULL,
		PRIMARY KEY (game_id, number)
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
	CREATE TABLE IF NOT EXISTS mc_instances (
		number        INTEGER PRIMARY KEY,               -- 1 to 4
		name          TEXT NOT NULL,
		slug          TEXT NOT NULL,
		seed          TEXT,
		loader        TEXT NOT NULL DEFAULT 'neoforge',  -- fabric|neoforge|vanilla
		source        TEXT NOT NULL DEFAULT 'modlist',   -- modpack|modlist|vanilla|import
		pack          TEXT,
		pack_provider TEXT,
		pack_ref      TEXT,
		mc_version    TEXT,
		tier          TEXT NOT NULL DEFAULT 'medium',    -- small|medium|large
		state         TEXT NOT NULL DEFAULT 'stopped',   -- running|stopped|provisioning|error
		motd          TEXT,
		difficulty    TEXT DEFAULT 'normal',
		gamemode      TEXT DEFAULT 'survival',
		world_type    TEXT DEFAULT 'default',
		max_players   INTEGER DEFAULT 20,
		lb_ip         TEXT,
		created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		last_used     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS valheim_instances (
		number        INTEGER PRIMARY KEY,
		name          TEXT NOT NULL,
		slug          TEXT NOT NULL,
		seed          TEXT,
		password      TEXT,
		tier          TEXT NOT NULL DEFAULT 'medium',
		state         TEXT NOT NULL DEFAULT 'stopped',
		motd          TEXT,
		max_players   INTEGER DEFAULT 10,
		lb_ip         TEXT,
		source        TEXT NOT NULL DEFAULT '',
		created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		last_used     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS users (
		username      TEXT PRIMARY KEY,
		password_hash TEXT NOT NULL,
		email         TEXT,
		created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS meta (
		key   TEXT PRIMARY KEY,
		value TEXT
	);
	CREATE TABLE IF NOT EXISTS accounts (
		subject    TEXT PRIMARY KEY,
		email      TEXT NOT NULL DEFAULT '',
		name       TEXT NOT NULL DEFAULT '',
		first_seen TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		last_seen  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_accounts_email ON accounts(email);
	CREATE TABLE IF NOT EXISTS sessions (
		id         TEXT PRIMARY KEY,
		subject    TEXT NOT NULL,
		email      TEXT NOT NULL DEFAULT '',
		name       TEXT NOT NULL DEFAULT '',
		roles      TEXT NOT NULL DEFAULT '',
		id_token   TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMP NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
	CREATE INDEX IF NOT EXISTS idx_sessions_subject ON sessions(subject);
	CREATE TABLE IF NOT EXISTS instance_requests (
		id         TEXT PRIMARY KEY,
		subject    TEXT NOT NULL,
		game_id    TEXT NOT NULL,
		name       TEXT NOT NULL,
		spec       TEXT NOT NULL,
		status     TEXT NOT NULL DEFAULT 'pending',
		note       TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		decided_at TIMESTAMP,
		decided_by TEXT NOT NULL DEFAULT '',
		number     INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_requests_status ON instance_requests(status);
	CREATE INDEX IF NOT EXISTS idx_requests_subject ON instance_requests(subject);
	CREATE TABLE IF NOT EXISTS tiers (
		game_id           TEXT NOT NULL,
		key               TEXT NOT NULL,
		name              TEXT NOT NULL DEFAULT '',
		mem_request_gib   INTEGER NOT NULL DEFAULT 0,
		mem_limit_gib     INTEGER NOT NULL DEFAULT 0,
		cpu_request_milli INTEGER NOT NULL DEFAULT 0,
		cpu_limit_milli   INTEGER NOT NULL DEFAULT 0,
		heap_init_gib     INTEGER NOT NULL DEFAULT 0,
		sort_order        INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (game_id, key)
	);
	CREATE TABLE IF NOT EXISTS game_settings (
		game_id           TEXT PRIMARY KEY,
		total_budget_gib  INTEGER NOT NULL DEFAULT 0,
		max_instances     INTEGER NOT NULL DEFAULT 0,
		max_running       INTEGER NOT NULL DEFAULT 0,
		ceiling_mem_gib   INTEGER NOT NULL DEFAULT 0,
		ceiling_cpu_milli INTEGER NOT NULL DEFAULT 0,
		updated_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`)
	if err != nil {
		return err
	}
	for _, stmt := range []string{
		`ALTER TABLE players ADD COLUMN online INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE players ADD COLUMN online_since TIMESTAMP`,
		`ALTER TABLE valheim_instances ADD COLUMN source TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE valheim_instances ADD COLUMN created_by TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE mc_instances ADD COLUMN created_by TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE valheim_instances ADD COLUMN mem_request_gib INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE valheim_instances ADD COLUMN mem_limit_gib INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE valheim_instances ADD COLUMN cpu_request_milli INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE valheim_instances ADD COLUMN cpu_limit_milli INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE mc_instances ADD COLUMN mem_request_gib INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE mc_instances ADD COLUMN mem_limit_gib INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE mc_instances ADD COLUMN cpu_request_milli INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE mc_instances ADD COLUMN cpu_limit_milli INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE mc_instances ADD COLUMN heap_init_gib INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := s.db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return nil
}
