package authstore

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mhersson/contextmatrix/internal/sqliteutil"
)

var migrations = []sqliteutil.Migration{
	{
		Version: 1,
		Up: func(ctx context.Context, db *sql.DB) error {
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
