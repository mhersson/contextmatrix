package authstore

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// migration is a single versioned schema change for auth.db.
type migration struct {
	version int
	up      func(ctx context.Context, db *sql.DB) error
}

var migrations = []migration{
	{
		version: 1,
		up: func(ctx context.Context, db *sql.DB) error {
			stmts := []string{
				`CREATE TABLE IF NOT EXISTS users (
					id            INTEGER PRIMARY KEY AUTOINCREMENT,
					username      TEXT    NOT NULL UNIQUE,
					display_name  TEXT    NOT NULL DEFAULT '',
					password_hash TEXT,
					is_admin      INTEGER NOT NULL DEFAULT 0,
					disabled      INTEGER NOT NULL DEFAULT 0,
					created_at    INTEGER NOT NULL,
					updated_at    INTEGER NOT NULL,
					last_login_at INTEGER
				)`,
				`CREATE TABLE IF NOT EXISTS sessions (
					token_hash   TEXT    PRIMARY KEY,
					user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
					created_at   INTEGER NOT NULL,
					expires_at   INTEGER NOT NULL,
					last_seen_at INTEGER NOT NULL
				)`,
				`CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id)`,
				`CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at)`,
				`CREATE TABLE IF NOT EXISTS one_time_tokens (
					token_hash TEXT    PRIMARY KEY,
					purpose    TEXT    NOT NULL,
					user_id    INTEGER REFERENCES users(id) ON DELETE CASCADE,
					created_at INTEGER NOT NULL,
					expires_at INTEGER NOT NULL,
					used_at    INTEGER
				)`,
				`CREATE INDEX IF NOT EXISTS idx_ott_user ON one_time_tokens(user_id)`,
				`CREATE TABLE IF NOT EXISTS credentials (
					name             TEXT    PRIMARY KEY,
					kind             TEXT    NOT NULL,
					host             TEXT    NOT NULL DEFAULT '',
					api_base_url     TEXT    NOT NULL DEFAULT '',
					app_id           INTEGER NOT NULL DEFAULT 0,
					installation_id  INTEGER NOT NULL DEFAULT 0,
					encrypted_secret BLOB    NOT NULL,
					created_by       TEXT    NOT NULL,
					disabled         INTEGER NOT NULL DEFAULT 0,
					created_at       INTEGER NOT NULL,
					updated_at       INTEGER NOT NULL,
					last_used_at     INTEGER
				)`,
			}

			for _, stmt := range stmts {
				if _, err := db.ExecContext(ctx, stmt); err != nil {
					return fmt.Errorf("authstore: create schema: %w", err)
				}
			}

			return nil
		},
	},
}

// migrate applies schema migrations using a schema_migrations ledger. It is
// idempotent: re-running against a fully migrated database is a no-op. Same
// pattern as internal/images.
func migrate(db *sql.DB) error {
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("authstore: schema_migrations table: %w", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("authstore: schema_migrations query: %w", err)
	}
	defer rows.Close()

	applied := map[int]bool{}

	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return fmt.Errorf("authstore: schema_migrations scan: %w", err)
		}

		applied[v] = true
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("authstore: schema_migrations rows: %w", err)
	}

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}

		if err := m.up(ctx, db); err != nil {
			return fmt.Errorf("authstore: migration v%d: %w", m.version, err)
		}

		if _, err := db.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			m.version, time.Now().Unix(),
		); err != nil {
			return fmt.Errorf("authstore: record migration v%d: %w", m.version, err)
		}
	}

	return nil
}
